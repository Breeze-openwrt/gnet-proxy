package outbound

import (
	"io"
	"net"
	"time" // 🆕 引入时间包，用于背压时的微秒级等待

	"github.com/panjf2000/gnet/v2"

	"gnet-proxy/pkg/common/logger"
)

/**
 * 🚛 [搬运工模块]：Transport
 * 它的职责非常纯粹：既然路已经打通了，剩下的就是把数据包从这边搬到那边。
 */
type Transport struct{}

func NewTransport() *Transport {
	return &Transport{}
}

/**
 * 🔄 RelayBack：下行转发逻辑（后端 -> 代理 -> 客户端）
 * 特点：负责把后端回来的数据（如下传的文件、网页内容）全速送回给客户端。
 */
func (t *Transport) RelayBack(c gnet.Conn, backendConn net.Conn) {
	// 🏠 [高速下载优化]：
	// 针对下载场景，我们将单次读取的缓冲区提升至 64KB (32KB * 2)。
	// 较大的缓冲区能显著减少系统调用次数，提升海量数据下载时的 CPU 效率。
	buf := make([]byte, 64*1024)
	
	// 💡 注意：这里没有使用 pool.Get() 是因为我们想在下载链路上使用更大的 64KB 块，
	// 而当前全局内存池是针对 32KB 优化的。
	
	for {
		// 🌊 [下行智能背压：控制水流速度]
		// 逻辑：如果客户端处理得慢，代码积压在 gnet 的发送缓冲区里超过 4MB，
		//我们就暂时停止从后端物理连接读取数据。
		// 这样做的好处：1. 防止内存溢出；2. 触发后端的 TCP 拥塞控制，让整个链路达到动态平衡。
		if c.OutboundBuffered() > 4*1024*1024 {
			time.Sleep(1 * time.Millisecond) // 稍微缓一缓，给客户端一点消化时间
			continue
		}

		// 📥 1. 从后端链接读取数据
		n, err := backendConn.Read(buf)
		if n > 0 {
			/**
			 * 🚀 [性能狂魔细节]：
			 * gnet 封装了底层的异步写入。AsyncWrite 实际上是把数据丢进了一个队列。
			 */
			// ⚠️ 极速下行时，内存申请是最大的性能开销。
			// 虽然这里还有一次 copy，但通过外层的背压控制，我们确保了总内存占用始终在可控范围内。
			dataToSend := make([]byte, n)
			copy(dataToSend, buf[:n])
			c.AsyncWrite(dataToSend, nil)
		}
		
		if err != nil {
			if err != io.EOF {
				logger.Debugf("⚠️ [下行异常] 来自后端的读取错误 (Client %s): %v", c.RemoteAddr(), err)
			}
			// 链路断了，赶紧通知 gnet 把客户端这边也关了
			c.Close()
			return
		}
	}
}

/**
 * 💡 [小知识：为什么下行背压能提高稳定性？]
 * 在不加流控的情况下，如果后端发货极快（1Gbps）而客户端接收极慢（1Mbps），
 * 代理程序会在内存里堆积进 GB 级的数据。这会导致代理进程被系统杀掉。
 * 现在的 4MB 阈值是一个“黄金平衡点”，既保证了流水线不会空转，又保护了系统安全。
 */

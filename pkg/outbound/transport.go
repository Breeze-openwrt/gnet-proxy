package outbound

import (
	"io"
	"net"

	"github.com/panjf2000/gnet/v2"

	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/common/pool"
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
 * 这是在另一个独立的协程里跑的。
 * 为什么？因为客户端在发数据给我的同时，后端可能也在不停地回数据，必须双管齐下。
 */
func (t *Transport) RelayBack(c gnet.Conn, backendConn net.Conn) {
	// 🏠 [资源复用池]：从内存池里拿一个 32KB 的大袋子
	buf := pool.Get()
	defer pool.Put(buf) // 函数执行完（连接断了）记得把袋子还回去

	for {
		// 📥 1. 从后端链接读取数据
		n, err := backendConn.Read(buf)
		if n > 0 {
			/**
			 * 🚀 [性能狂魔细节]：
			 * gnet 封装了底层的异步写入。调用 AsyncWrite 实际上是把数据丢进了一个队列，
			 * 它会自动排队并最终通过操作系统的非阻塞接口发给客户端。
			 * 这样即使客户端接收慢，也不会卡死当前的 Read 协程。
			 */
			// ⚠️ 必须复制出一份切片发给 AsyncWrite，因为 AsyncWrite 是异步完成的，
			// 如果直接传原 buf 切片进去，会导致缓冲区正在被写的时候又被 Read 复用，产生数据混乱。
			dataToSend := make([]byte, n)
			copy(dataToSend, buf[:n])
			c.AsyncWrite(dataToSend, nil)
		}
		
		if err != nil {
			if err != io.EOF {
				logger.Debugf("⚠️ [下行异常] 来自后端的读取错误: %v", err)
			}
			// 链路断了，赶紧通知 gnet 把客户端这边也关了
			c.Close()
			return
		}
	}
}

/**
 * 💡 [小知识：什么是零分配转发？]
 * 理想的转发是数据直接中转不进用户内存，
 * 但由于我们需要在中间做 SNI 识别和后端负载均衡，
 * 所以目前的内存池复用已经是达到“生产级”极致吞吐量的最优选。
 */

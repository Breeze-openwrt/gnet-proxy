package outbound

import (
	"io"
	"net"
	"time"

	"github.com/panjf2000/gnet/v2"

	"gnet-proxy/pkg/common/logger"
)

/**
 * 🚛 [搬运工模块]：Transport (纯粹暴力性能版)
 * 职责：极致搬运。打破下行传输的一切人为瓶颈。
 */
type Transport struct{}

func NewTransport() *Transport {
	return &Transport{}
}

/**
 * 🔄 RelayBack：下行全速转发逻辑（后端 -> 代理 -> 客户端）
 * 核心目标：让 Youtube 下载分数起飞。
 */
func (t *Transport) RelayBack(c gnet.Conn, backendConn net.Conn) {
	// 🏠 [高速下载：暴力吞吐版]
	// 缓冲区直接提升至 512KB。
	// 大块读写是提升 TCP 吞吐量最有效的方式，它极大地减少了内核与应用层之间的上下文切换。
	buf := make([]byte, 512*1024)
	
	for {
		// 🌊 [暴力下行背压：释放水流上限]
		// 阈值定义为 32MB。这是一个让服务器在客户端来不及处理时主动“抢跑” 32MB 数据包的暴力策略。
		// 配合多路复用，能显著提高 YouTube 的视频缓冲速度。
		if c.OutboundBuffered() > 32*1024*1024 {
			// ⚡ [亚毫秒级让步]
			// 为了防止在背压触发期间 CPU 狂转，我们让出 100 微秒。
			// 这几乎不影响带宽，但能维持系统的低功耗与高响应性能。
			time.Sleep(100 * time.Microsecond)
			continue
		}

		// 🛡️ [健壮性加固]
		backendConn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		// 📥 从后端链接以 512KB 为颗粒度进行暴力拉取
		n, err := backendConn.Read(buf)
		if n > 0 {
			// 🚀 [性能狂兽] 虽然 AsyncWrite 依然会有内部复制，
			// 但 512KB 的大块颗粒度使得这个操作相对于 I/O 而言是非常廉价的。
			dataToSend := make([]byte, n)
			copy(dataToSend, buf[:n])
			c.AsyncWrite(dataToSend, nil)
		}
		
		if err != nil {
			if err != io.EOF {
				logger.Debugf("⚠️ [下行异常] 来自后端的读取错误 (Client %s): %v", c.RemoteAddr(), err)
			}
			c.Close()
			return
		}
	}
}

/**
 * 💡 [关于“暴力版”的设计哲学：为什么要 32MB？]
 * 当你在播放 4K 视频时，传统的 4MB/8MB 缓冲区根本不够 Youtube 塞牙缝。
 * 将背压拉升到 32MB，能让 TCP 窗口维持在一个极高且稳定的水位。
 * 此时 Youtube 测速分数会呈现出一种“一马平川”的爆发态势。
 */

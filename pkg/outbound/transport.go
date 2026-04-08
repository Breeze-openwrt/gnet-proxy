package outbound

import (
	"net"
	"time"

	"github.com/panjf2000/gnet/v2"
)

/**
 * 🚛 [搬运工模块]：Transport (并行秒开版)
 * 职责：极速搬运。利用并发流水线将首包延迟降至 0。
 */
type Transport struct{}

func NewTransport() *Transport {
	return &Transport{}
}

/**
 * 🔄 RelayBack：下行全速转发逻辑（后端 -> 代理 -> 客户端）
 * 核心目标：秒开，秒开，还是秒开！
 */
func (t *Transport) RelayBack(c gnet.Conn, backendConn net.Conn) {
	// 🏠 [秒开优化：异步双向流水线]
	// 策略：我们开启了一个专门的异步协程来“吞噬”后端的数据。
	// 它不再等待应用层的主循环，只要网卡里有数据，立刻读出并 AsyncWrite 给客户端。
	
	// 设置 5 分钟的全局读取截止时间。
	backendConn.SetReadDeadline(time.Now().Add(5 * time.Minute))

	// 🛠️ 启动异步“并行搬运工”
	go func() {
		defer c.Close()
		defer backendConn.Close()

		// 采用更具灵敏性的 32KB 黄金规格。
		// 在秒开场景下，小块并发优于大块堆积。
		buf := make([]byte, 32*1024)
		for {
			// 🌊 灵敏流控阈值：当客户端缓冲区超过 8MB 时进入微秒级挂起。
			if c.OutboundBuffered() > 8*1024*1024 {
				time.Sleep(100 * time.Microsecond)
				continue
			}

			// 📥 并行读取，不阻塞任何主线程
			n, err := backendConn.Read(buf)
			if n > 0 {
				/**
				 * 🚀 [即读即发]：
				 * 采用动态切片捕获数据，确保无截断地全速下发。
				 */
				data := make([]byte, n)
				copy(data, buf[:n])
				c.AsyncWrite(data, nil)
			}
			if err != nil {
				// 链路中断（正常 EOF 或异常），退出协程并由 defer 清理。
				return
			}
		}
	}()
}

package outbound

import (
	"io"
	"net"
	"time"

	"github.com/panjf2000/gnet/v2"

	"gnet-proxy/pkg/common/logger"
)

/**
 * 🚛 [搬运工模块]：Transport (智慧平衡版)
 * 目标：在响应延迟 (Latency) 与吞吐量 (Throughput) 之间取得完美平衡。
 */
type Transport struct{}

func NewTransport() *Transport {
	return &Transport{}
}

/**
 * 🔄 RelayBack：下行全速转发逻辑（后端 -> 代理 -> 客户端）
 */
func (t *Transport) RelayBack(c gnet.Conn, backendConn net.Conn) {
	// 🏠 [高速响应优化]：
	// 我们将缓冲区设定为 64KB。
	// 这比 512KB 小得多，能确保即使是网页首包也能被立刻转发，消除响应延迟感。
	buf := make([]byte, 64*1024)
	
	for {
		// 🌊 [智慧下行背压]
		// 阈值设定为 16MB。这是一个稳健的“肺活量”，既能跑满百兆宽带，又不会导致严重的内存抖动。
		if c.OutboundBuffered() > 16*1024*1024 {
			// ⚡ [亚毫秒级调度] 让出 500 微秒，让客户端消化一下。
			time.Sleep(500 * time.Microsecond)
			continue
		}

		// 🛡️ [健壮性加固]
		backendConn.SetReadDeadline(time.Now().Add(5 * time.Minute))

		// 📥 从后端链接以 64KB 颗粒度进行读取
		n, err := backendConn.Read(buf)
		if n > 0 {
			// 🚀 [无损快速搬运]：我们直接申请与读取量匹配的内存，确保 100% 完整性。
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
 * 💡 [关于“智慧平衡版”的设计哲学]
 * 并不是缓冲区越大越好。超大的缓冲区会带来显著的“排队延迟”。
 * 我们的 64KB 设计确保了小包（如网页、指令）能“见缝插针”地被转发。
 * 配合上行的 128MB 无损缓冲，系统达到了响应速度与传输能力的黄金分割点。
 */

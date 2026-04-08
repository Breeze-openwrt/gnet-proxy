package outbound

import (
	"io"
	"net"

	"gnet-proxy/pkg/common/logger"

	"github.com/panjf2000/gnet/v2"
)

// Transport 封装底层异步传输层能力
type Transport struct{}

func NewTransport() *Transport {
	return &Transport{}
}

// RelayBack 执行反向转发：后端 -> 转发器 -> 客户端 (纯下行脱壳协程)
func (t *Transport) RelayBack(c gnet.Conn, backend net.Conn) {
	defer backend.Close()
	// ⚠️ 极其致命的坑：这里绝不能复用单一的 buf 给 AsyncWrite 原地使用！
	// gnet 的 AsyncWrite 是纯异步的，它将切片直接放到环形队列而不是立刻发走。
	// 如果用 bufferPool 并且循环 Read，下一次的 Read 会直接覆盖上一次还没发出去的数据，
	// 导致客户端收到一堆被破坏重叠的乱码，这也是为什么 TLS 层会校验失败并突然断开！
	buf := make([]byte, 32*1024)
	for {
		n, err := backend.Read(buf)
		if err != nil {
			if err != io.EOF {
				logger.Errorf("❌ [网络错误] 从后端读取流被中断 (Backend -> Client %s): %v", c.RemoteAddr(), err)
			} else {
				logger.Debugf("✅ [正常关闭] 后端数据传输完毕并断开 (Backend -> Client %s)", c.RemoteAddr())
			}
			break
		}

		logger.Tracef("⬇️ [下行数据] (Backend -> Client %s) 收到并回传 %d 字节", c.RemoteAddr(), n)

		// 必须执行非常严格的深拷贝 (Deep Copy)，确保移交给 AsyncWrite 的内容绝对安全
		dataCopy := make([]byte, n)
		copy(dataCopy, buf[:n])

		err = c.AsyncWrite(dataCopy, nil)
		if err != nil {
			logger.Errorf("❌ [回传异常] 写回客户端失败 (Client %s): %v", c.RemoteAddr(), err)
			break
		}
	}
	// 唤醒 Reactor
	c.Wake(nil)
}

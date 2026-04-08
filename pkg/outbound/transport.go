package outbound

import (
	"io"
	"net"

	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/common/pool"

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
	for {
		// 🚀 [性能狂魔] 从内存池中获取缓冲区，避免频繁创建销毁
		buf := pool.Get()
		n, err := backend.Read(buf)
		if err != nil {
			pool.Put(buf) // 即使失败也要归还
			if err != io.EOF {
				logger.Errorf("❌ [网络错误] 从后端读取流被中断 (Backend -> Client %s): %v", c.RemoteAddr(), err)
			} else {
				logger.Debugf("✅ [正常关闭] 后端数据传输完毕并断开 (Backend -> Client %s)", c.RemoteAddr())
			}
			break
		}

		logger.Tracef("⬇️ [下行数据] (Backend -> Client %s) 收到并回传 %d 字节", c.RemoteAddr(), n)

		// 🚢 [零拷贝优化] 这里的切片 buf[:n] 直接交给 gnet，
		// 配合回调函数在数据发送彻底完成后再归还池，完美解决异步数据污染问题。
		err = c.AsyncWrite(buf[:n], func(c gnet.Conn, err error) error {
			pool.Put(buf)
			return nil
		})
		if err != nil {
			logger.Errorf("❌ [回传异常] 写回客户端失败 (Client %s): %v", c.RemoteAddr(), err)
			// 注意：如果 AsyncWrite 本身报错，回调可能不会执行，这里需要视具体 gnet 版本实现决定是否手动归还
			// 在 gnet v2 中，建议在此处安全处理
			break
		}
	}
	// 真正的物理断开：后端既然关了，告诉 gnet 直接杀掉前端连接，不要用 tcp 的 keepalive 熬 5 分钟
	c.Close()
}

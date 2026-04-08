package main

import (
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2"
	"github.com/rs/zerolog/log"
)

// 🧐 gnet-proxy：极致性能 Zero-Allocation 日志版
// 核心优化：引入 rs/zerolog，通过无锁化和强类型输出，彻底消除日志产生的性能损耗。

const (
	LogLevelSilent = 0
	LogLevelInfo   = 1
	LogLevelDebug  = 2
	LogLevelTrace  = 3
)

type proxyServer struct {
	gnet.BuiltinEventEngine
	addr       string
	multicore  bool
	verbosity  int
	routes     map[string]RouteRule
	bufferPool sync.Pool
}

type connContext struct {
	backendConn net.Conn
	isProxying  bool
}

// 🛡️ 极致性能日志包装器 (零分配重构)
// 只有在满足日志级别时才会执行 Msgf，减少格式化开销
func (s *proxyServer) tracef(format string, v ...interface{}) {
	if s.verbosity >= LogLevelTrace {
		log.Trace().Msgf(format, v...)
	}
}

func (s *proxyServer) debugf(format string, v ...interface{}) {
	if s.verbosity >= LogLevelDebug {
		log.Debug().Msgf(format, v...)
	}
}

func (s *proxyServer) infof(format string, v ...interface{}) {
	if s.verbosity >= LogLevelInfo {
		log.Info().Msgf(format, v...)
	}
}

func (s *proxyServer) errorf(format string, v ...interface{}) {
	if s.verbosity >= LogLevelInfo {
		log.Error().Msgf(format, v...)
	}
}

// dialBackend 用作后端的极速拨号器
func (s *proxyServer) dialBackend(rule RouteRule) (net.Conn, error) {
	network := "tcp"
	target := rule.Addr
	if strings.HasPrefix(target, "unix://") {
		network = "unix"
		target = strings.TrimPrefix(target, "unix://")
	} else if strings.HasPrefix(target, "tcp://") {
		network = "tcp"
		target = strings.TrimPrefix(target, "tcp://")
	}
	// 🛡️ [长连接保障] 使用自定义 Dialer 显式开启 TCP Keep-Alive
	dialer := &net.Dialer{
		Timeout:   3 * time.Second,
		KeepAlive: 5 * time.Minute, // 每 5 分钟发送一次保活探测包
	}
	return dialer.Dial(network, target)
}

// proxyBack 执行反向转发：后端 -> 转发器 -> 客户端 (纯下行脱壳协程)
func (s *proxyServer) proxyBack(c gnet.Conn, backend net.Conn) {
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
				// 这个是经常出现的连接被重置等底层网络错误
				s.errorf("❌ [网络错误] 从后端读取流被中断 (Backend -> Client %s): %v", c.RemoteAddr(), err)
			} else {
				s.debugf("✅ [正常关闭] 后端数据传输完毕并断开 (Backend -> Client %s)", c.RemoteAddr())
			}
			break
		}

		s.tracef("⬇️ [下行数据] (Backend -> Client %s) 收到并回传 %d 字节", c.RemoteAddr(), n)

		// 必须执行非常严格的深拷贝 (Deep Copy)，确保移交给 AsyncWrite 的内容绝对安全
		dataCopy := make([]byte, n)
		copy(dataCopy, buf[:n])

		err = c.AsyncWrite(dataCopy, nil)
		if err != nil {
			s.errorf("❌ [回传异常] 写回客户端失败 (Client %s): %v", c.RemoteAddr(), err)
			break
		}
	}
	// 唤醒 Reactor
	c.Wake(nil)
}

package inbound

import (
	"net"

	"github.com/panjf2000/gnet/v2"

	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/common/sniffer"
	"gnet-proxy/pkg/config"
	"gnet-proxy/pkg/core"
	"gnet-proxy/pkg/outbound"
)

// Server 代表了入站监听引擎
type Server struct {
	gnet.BuiltinEventEngine
	addr      string
	multicore bool
	router    *core.Router
	dialer    *outbound.Dialer
	transport *outbound.Transport
}

// connContext 会附着在连接上以记录后端状态
type connContext struct {
	backendConn net.Conn
	isDialing   bool
	isProxying  bool
	writeChan   chan []byte
}

// NewServer 构造函数，依赖注入 Router 和出站组件
func NewServer(addr string, multicore bool, router *core.Router, dialer *outbound.Dialer, transport *outbound.Transport) *Server {
	return &Server{
		addr:      addr,
		multicore: multicore,
		router:    router,
		dialer:    dialer,
		transport: transport,
	}
}

// Run 挂载运行 Server (非阻塞/阻塞取决于具体实现。这里沿用主阻塞逻辑)
func (s *Server) Run() error {
	// 🛡️ [长连接保障] 显式告知 gnet 为所有客户端连接开启 TCP Keep-Alive 保活机制
	return gnet.Run(s, "tcp://"+s.addr,
		gnet.WithMulticore(s.multicore),
		gnet.WithTCPNoDelay(gnet.TCPNoDelay), // 开启 TCP_NODELAY 降低小包延迟
	)
}

func (s *Server) OnBoot(eng gnet.Engine) gnet.Action {
	logger.Infof("🚀 gnet-proxy 极速分流器启动成功！监听: %s", s.addr)
	logger.Infof("🧠 多核模式: %v", s.multicore)
	return gnet.None
}

func (s *Server) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	logger.Infof("🔌 [接入] 新客户端: %s", c.RemoteAddr())
	return nil, gnet.None
}

func (s *Server) OnTraffic(c gnet.Conn) gnet.Action {
	ctx := c.Context()
	if ctx == nil {
		buf, _ := c.Peek(-1)

		// 极其关键：TLS 数据在开始传输前必然需要收集齐
		if len(buf) == 0 {
			return gnet.None
		}

		sni, err := sniffer.ParseSNI(buf)
		if err != nil {
			if err == sniffer.ErrIncompletePacket {
				// 握手包还没接收完，继续包组装
				return gnet.None
			}
			logger.Infof("❓ [无域名/非TLS流量] 客户端 %s 的流量未识别到 SNI (原因: %v)，将尝试 Fallback 回退路由", c.RemoteAddr(), err)
		} else {
			logger.Infof("🔍 [SNI 提取成功] 客户端 %s 识别到域名: %s", c.RemoteAddr(), sni)
		}

		// 使用路由核心去决策
		rule, ok := s.router.Match(sni, c.RemoteAddr().String())
		if !ok {
			return gnet.Close
		}

		// 🚀 [非阻塞优化] 开启异步拨号，立即释放当前并发 Loop
		newCtx := &connContext{isDialing: true, writeChan: make(chan []byte, 128)}
		c.SetContext(newCtx)

		// 将这些初始握手数据也记录下来，等拨号成功后补发
		firstPacket, _ := c.Next(-1)
		firstPacketCopy := make([]byte, len(firstPacket))
		copy(firstPacketCopy, firstPacket)
		newCtx.writeChan <- firstPacketCopy

		go s.asyncDial(c, newCtx, rule)
		return gnet.None
	}

	pCtx := ctx.(*connContext)
	if pCtx.isDialing {
		// 正在拨号中，继续接收并排队后续数据，防止数据丢失
		msg, _ := c.Next(-1)
		if len(msg) > 0 {
			msgCopy := make([]byte, len(msg))
			copy(msgCopy, msg)
			select {
			case pCtx.writeChan <- msgCopy:
			default:
				// 如果队列满了，说明后端或者拨号太慢，为了安全只能掐断
				logger.Errorf("❌ [拥塞] 客户端 %s 发送太快但拨号未完成，强制断开", c.RemoteAddr())
				return gnet.Close
			}
		}
		return gnet.None
	}

	if pCtx.isProxying {
		msg, _ := c.Next(-1)
		if len(msg) > 0 {
			// 🚀 [非阻塞优化] 将数据丢进管道，由专门的 relayUp 协程负责写入，绝对不阻塞 Event-Loop
			msgCopy := make([]byte, len(msg))
			copy(msgCopy, msg)
			select {
			case pCtx.writeChan <- msgCopy:
			default:
				logger.Errorf("❌ [拥塞] 发送至后端管道已满 (Client %s)", c.RemoteAddr())
				return gnet.Close
			}
		}
	}

	return gnet.None
}

// asyncDial 在后台执行阻塞的系统调用 Dial
func (s *Server) asyncDial(c gnet.Conn, ctx *connContext, rule config.RouteRule) {
	backendConn, err := s.dialer.Dial(rule)
	if err != nil {
		logger.Errorf("❌ [拨号失败] 无法连接到后端 %s (客户端 %s): %v", rule.Addr, c.RemoteAddr(), err)
		c.Close()
		return
	}

	logger.Infof("✅ [拨号成功] 已连通后端 %s (客户端 %s)", rule.Addr, c.RemoteAddr())

	ctx.backendConn = backendConn
	ctx.isDialing = false
	ctx.isProxying = true

	// 启动双向转发拦截
	go s.transport.RelayBack(c, backendConn)
	go s.relayUp(c, ctx)

	// 唤醒 gnet 检查是否有残留数据需要处理
	c.Wake(nil)
}

// relayUp 负责将管道中的数据同步写入后端，允许在遇到网络瓶颈时阻塞在该协程中，而不影响其他连接
func (s *Server) relayUp(c gnet.Conn, ctx *connContext) {
	defer ctx.backendConn.Close()
	defer func() {
		close(ctx.writeChan)
		// 清理剩余数据以防泄露
		for range ctx.writeChan {}
	}()
	
	for msg := range ctx.writeChan {
		_, err := ctx.backendConn.Write(msg)
		if err != nil {
			logger.Errorf("❌ [上行异常] 发送数据到后端失败 (Client %s): %v", c.RemoteAddr(), err)
			c.Close()
			return
		}
	}
}

func (s *Server) OnClose(c gnet.Conn, err error) gnet.Action {
	if err != nil {
		logger.Errorf("❌ [连接断开] 客户端异常断开 (Client %s): %v", c.RemoteAddr(), err)
	} else {
		logger.Infof("👋 [连接关闭] 客户端正常断开 (Client %s)", c.RemoteAddr())
	}

	if c.Context() != nil {
		pCtx := c.Context().(*connContext)
		if pCtx.backendConn != nil {
			logger.Debugf("🧹 [清理] 销毁与后端的连接 (Client %s)", c.RemoteAddr())
			pCtx.backendConn.Close()
		}
	}
	return gnet.None
}

package inbound

import (
	"net"

	"github.com/panjf2000/gnet/v2"

	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/common/sniffer"
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
	isProxying  bool
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
	if c.Context() == nil {
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

		// 出站拨号器接管
		backendConn, err := s.dialer.Dial(rule)
		if err != nil {
			logger.Errorf("❌ [拨号失败] 无法连接到后端 %s (客户端 %s): %v", rule.Addr, c.RemoteAddr(), err)
			return gnet.Close
		}
		logger.Infof("✅ [拨号成功] 已连通后端 %s (客户端 %s)", rule.Addr, c.RemoteAddr())

		newCtx := &connContext{backendConn: backendConn, isProxying: true}
		c.SetContext(newCtx)

		// 独立的传输引擎启动下行接驳协程
		go s.transport.RelayBack(c, backendConn)
	}

	pCtx := c.Context().(*connContext)
	msg, _ := c.Next(-1)

	// 记录请求转发量（只有在 trace 级别，也就是 -vvv 时才大规模刷屏显示字节数，避免性能损耗）
	logger.Tracef("⬆️ [上行数据] (Client %s -> Backend) 转发了 %d 字节", c.RemoteAddr(), len(msg))

	_, err := pCtx.backendConn.Write(msg)
	if err != nil {
		logger.Errorf("❌ [转发异常] 发送数据到后端失败 (Client %s): %v", c.RemoteAddr(), err)
		return gnet.Close
	}
	return gnet.None
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

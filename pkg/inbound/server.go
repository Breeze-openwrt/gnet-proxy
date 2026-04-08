package inbound

import (
	"net"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2"

	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/common/pool"
	"gnet-proxy/pkg/common/sniffer"
	"gnet-proxy/pkg/config"
	"gnet-proxy/pkg/core"
	"gnet-proxy/pkg/outbound"
)

// connContext 每一个入站连接的“私人管家”
type connContext struct {
	backendConn net.Conn      // 后端连接实体
	isDialing   bool          // 是否正在拨号中
	isProxying  bool          // 是否进入代理转发阶段
	writeChan   chan []byte   // 字节流管道，用于缓存待发送给后端的数据
	closeOnce   sync.Once     // 确保清理工作只做一次
}

// Server 入站服务引擎
type Server struct {
	*gnet.BuiltinEventEngine
	listenAddr string
	multicore  bool
	router     *core.Router
	dialer     *outbound.Dialer
	pool       *outbound.ConnectionPool
	transport  *outbound.Transport
}

func NewServer(addr string, multicore bool, router *core.Router, dialer *outbound.Dialer, pool *outbound.ConnectionPool, transport *outbound.Transport) *Server {
	return &Server{
		listenAddr: addr,
		multicore:  multicore,
		router:     router,
		dialer:     dialer,
		pool:       pool,
		transport:  transport,
	}
}

func (s *Server) Run() error {
	logger.Infof("🚀 [点火] 极速转发引擎启动于 %s (多核模式: %v)", s.listenAddr, s.multicore)
	return gnet.Run(s, s.listenAddr,
		gnet.WithMulticore(s.multicore),
		gnet.WithReusePort(true),
		gnet.WithTCPNoDelay(gnet.TCPNoDelay),
	)
}

// OnBoot：系统启动时的欢迎辞
func (s *Server) OnBoot(eng gnet.Engine) gnet.Action {
	logger.Infof("✅ [运行中] 核心事件循环已就绪")
	return gnet.None
}

/**
 * 🔥 [性能狂魔核心逻辑]：OnTraffic
 * 为了解决 git push 断流，我们采用了“极速全量吸纳”模式。
 * 核心原理：无论下游发得快慢，我们第一时间把内核缓冲区读空，杜绝 CPU 空转。
 */
func (s *Server) OnTraffic(c gnet.Conn) gnet.Action {
	ctx := c.Context()
	
	// --- 第一阶段：识别与初始化 ---
	if ctx == nil {
		buf, _ := c.Peek(-1)
		if len(buf) == 0 {
			return gnet.None
		}

		sni, err := sniffer.ParseSNI(buf)
		if err != nil && err != sniffer.ErrIncompletePacket {
			logger.Infof("❓ [未识别] 客户端 %s 的流量无 SNI，将使用默认路由", c.RemoteAddr())
		}

		rule, ok := s.router.Match(sni, c.RemoteAddr().String())
		if !ok {
			return gnet.Close
		}

		// 🚀 [生产级配比：128MB 瞬间缓冲极限]
		// 计算：4096 slots * 32KB/slot = 128MB。
		// 这能保证在大型 Pack 文件推送时，代理有足够的“胃口”吞掉数据。
		newCtx := &connContext{isDialing: true, writeChan: make(chan []byte, 4096)}
		c.SetContext(newCtx)

		// 🚢 [海吸动作]：立刻把识别用的第一包数据读走，不留在内核里。
		firstPacket, _ := c.Next(-1) 
		firstPacketCopy := pool.Get()
		n := copy(firstPacketCopy, firstPacket)
		newCtx.writeChan <- firstPacketCopy[:n]

		// 异步拨号
		go s.asyncDial(c, newCtx, rule)
		return gnet.None
	}

	// --- 第二阶段：极速吸纳管道 ---
	pCtx := ctx.(*connContext)
	
	// 🌊 [核心改进：杜绝 CPU 空转]
	// 不管 pCtx.isDialing 还是 pCtx.isProxying，我们先暴力读空内核缓冲区。
	msg, _ := c.Next(-1)
	if len(msg) == 0 {
		return gnet.None
	}
	
	msgCopy := pool.Get()
	n := copy(msgCopy, msg)

	// 将读到的数据塞进 128MB 缓冲管
	select {
	case pCtx.writeChan <- msgCopy[:n]:
		// 成功入队，由后台协程慢慢吐槽
	default:
		// 如果 128MB 都塞不进去了，说明后端或者客户端链路已经彻底瘫痪
		pool.Put(msgCopy)
		logger.Errorf("⚠️ [拥塞严重] 128MB 缓冲已爆满，强制断开以保护系统 (Client %s)", c.RemoteAddr())
		return gnet.Close
	}

	return gnet.None
}

/**
 * 📞 asyncDial：后台拨号助手
 */
func (s *Server) asyncDial(c gnet.Conn, ctx *connContext, rule config.RouteRule) {
	backendConn, err := s.pool.Acquire(rule)
	if err != nil {
		logger.Errorf("❌ [拨号失败] 无法连通 %s: %v", rule.Addr, err)
		c.Close()
		return
	}

	ctx.backendConn = backendConn
	ctx.isDialing = false
	ctx.isProxying = true

	// 1. RelayBack：后端回复 -> 发回前端
	go s.transport.RelayBack(c, backendConn)
	// 2. relayUp：前端暂存的数据 -> 写入后端
	go s.relayUp(c, ctx)

	// 激活读取
	c.Wake(nil)
}

/**
 * 🚚 relayUp：上行搬运工
 */
func (s *Server) relayUp(c gnet.Conn, ctx *connContext) {
	defer func() {
		for msg := range ctx.writeChan {
			pool.Put(msg)
		}
	}()

	for msg := range ctx.writeChan {
		// 设置 5 分钟超时，防止僵尸连接
		ctx.backendConn.SetWriteDeadline(time.Now().Add(5 * time.Minute))

		_, err := ctx.backendConn.Write(msg)
		pool.Put(msg)
		if err != nil {
			logger.Errorf("❌ [上行异常] 写入后端失败: %v", err)
			c.Close()
			return
		}
	}
}

// OnClose：清理资源
func (s *Server) OnClose(c gnet.Conn, err error) gnet.Action {
	if c.Context() != nil {
		pCtx := c.Context().(*connContext)
		pCtx.closeOnce.Do(func() {
			if pCtx.backendConn != nil {
				pCtx.backendConn.Close()
			}
			if pCtx.writeChan != nil {
				close(pCtx.writeChan)
			}
		})
	}
	return gnet.None
}

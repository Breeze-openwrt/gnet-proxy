package inbound

import (
	"net"
	"sync"

	"github.com/panjf2000/gnet/v2" // 🚀 极高性能的轻量级网络框架，其核心是“事件循环”模型

	"gnet-proxy/pkg/common/logger" // 日志模块
	"gnet-proxy/pkg/common/pool"   // 内存复用池，减少频繁申请 []byte 导致的 GC 压力
	"gnet-proxy/pkg/common/sniffer" // 流量嗅探器，专门用来偷看数据包里的域名(SNI)
	"gnet-proxy/pkg/config"         // 配置模型
	"gnet-proxy/pkg/core"           // 路由分发核心
	"gnet-proxy/pkg/outbound"       // 出站/转发模块
)

/**
 * 🧱 [架构解析]：
 * Server 是本程序的心脏。它运行在 gnet 的事件循环之上。
 * 每一个连接都会经过这里：接入 -> 嗅探域名 -> 匹配路由 -> 异步拨号 -> 建立双向透明转发。
 */
type Server struct {
	gnet.BuiltinEventEngine             // 继承自 gnet，获得默认的事件处理能力
	addr      string                     // 监听地址，如 "0.0.0.0:443"
	multicore bool                       // 是否开启多核并进模式
	router    *core.Router               // 这里的路由大脑，决定流量去哪
	dialer    *outbound.Dialer           // 拨号员
	pool      *outbound.ConnectionPool   // 连接池仓库
	transport *outbound.Transport        // 搬运工模块
}

/**
 * 🎒 [连接上下文]：
 * 这是一个“随身包裹”，每个客户端连接从进来那一刻起就会携带这个包裹，
 * 用来记录它现在进行到哪一步了（是正在拨号？还是已经开始传数据了？）。
 */
type connContext struct {
	backendConn net.Conn        // 与后端（比如 Xray/Mihomo）建立的真实 TCP 连接
	isDialing   bool            // 状态机标志：是否正在给后端打电话（拨号中）
	isProxying  bool            // 状态机标志：是否已经进入“左右互搏”的代理转发阶段
	writeChan   chan []byte     // 📦 这是一个关键的“中转站”，存储那些还没来得及发给后端的数据
	closeOnce   sync.Once       // 确保每个连接只清理一次资源的保险丝
}

// NewServer：依赖注入构造函数，组件化设计的典型体现
func NewServer(addr string, multicore bool, router *core.Router, dialer *outbound.Dialer, pool *outbound.ConnectionPool, transport *outbound.Transport) *Server {
	return &Server{
		addr:      addr,
		multicore: multicore,
		router:    router,
		dialer:    dialer,
		pool:      pool,
		transport: transport,
	}
}

// Run：启动引擎。一旦启动，整个主协程就会阻塞在这里等待网络事件。
func (s *Server) Run() error {
	// TCP_NODELAY 的作用：小数据包立即发出，不等待缓冲区存满，对秒开网页非常重要。
	return gnet.Run(s, "tcp://"+s.addr,
		gnet.WithMulticore(s.multicore),
		gnet.WithTCPNoDelay(gnet.TCPNoDelay), 
	)
}

// OnBoot：系统点火成功后的第一个动作。
func (s *Server) OnBoot(eng gnet.Engine) gnet.Action {
	logger.Infof("🚀 gnet-proxy 极速分流器启动成功！监听: %s", s.addr)
	logger.Infof("🧠 多核模式: %v", s.multicore)
	return gnet.None
}

// OnOpen：每当有一个新的手机或电脑尝试连接这个代理，这个函数就会被触发。
func (s *Server) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	logger.Infof("🔌 [接入] 新客户端: %s", c.RemoteAddr())
	return nil, gnet.None
}

/**
 * 🔥 [性能狂魔核心逻辑]：OnTraffic
 * 当连接上有数据包飞过来时，gnet 会调用这个函数。
 * 这是整个软件最复杂也最高效的地方。
 */
func (s *Server) OnTraffic(c gnet.Conn) gnet.Action {
	ctx := c.Context()
	
	// --- 第一阶段：初次相遇，身份识别 ---
	if ctx == nil {
		// 窥探一下缓冲区，不拿走数据，看看里面写了什么（Peek 是不消耗数据的）
		buf, _ := c.Peek(-1)
		if len(buf) == 0 {
			return gnet.None
		}

		// 🔍 核心动作：尝试从这一堆二进制里识别出它想访问哪个域名 (SNI)
		sni, err := sniffer.ParseSNI(buf)
		if err != nil {
			if err == sniffer.ErrIncompletePacket {
				// 握手包还没传完整，告诉 gnet 以后再试
				return gnet.None
			}
			logger.Infof("❓ [未识别] 客户端 %s 的流量无 SNI，将使用保底路由", c.RemoteAddr())
		} else {
			logger.Infof("🔍 [识别成功] 域名: %s (来自客户端: %s)", sni, c.RemoteAddr())
		}

		// 🧭 路由决策：去指挥部问问这个域名该分发到哪个后端
		rule, ok := s.router.Match(sni, c.RemoteAddr().String())
		if !ok {
			return gnet.Close // 没路由匹配，说明是不合法的业务，直接断开！
		}

		// 🚀 [非阻塞优化]：建立上下文并标记正在拨号。
		// 给每个连接配备一个缓存 128 个数据包的管道，防止后端卡顿时堵塞前台。
		newCtx := &connContext{isDialing: true, writeChan: make(chan []byte, 128)}
		c.SetContext(newCtx)

		// 抢救第一包数据：把刚刚看到的识别数据拿出来，复制一份存进管道。
		// gnet 的 buffer 是公用的，所以必须用 pool.Get() 复制走。
		firstPacket, _ := c.Next(-1) 
		firstPacketCopy := pool.Get()
		n := copy(firstPacketCopy, firstPacket)
		newCtx.writeChan <- firstPacketCopy[:n]

		// ⚠️ 极其重要：异步拨号！不能在当前主线程里拨号，否则一个慢域名会卡死所有人。
		go s.asyncDial(c, newCtx, rule)
		return gnet.None
	}

	// --- 第二阶段：拨号中，数据暂存 ---
	pCtx := ctx.(*connContext)
	if pCtx.isDialing {
		// 虽然还没连上后端，但客户端已经在发后续数据了，我们得先帮它存着
		msg, _ := c.Next(-1)
		if len(msg) > 0 {
			msgCopy := pool.Get()
			n := copy(msgCopy, msg)
			select {
			case pCtx.writeChan <- msgCopy[:n]:
			default:
				// 如果 128 个槽位都满了拨号还没好，说明网络太烂，为了系统稳定必须“绝交”
				pool.Put(msgCopy)
				logger.Errorf("❌ [拥塞] 缓冲区已爆满，强制断开 (Client %s)", c.RemoteAddr())
				return gnet.Close
			}
		}
		return gnet.None
	}

	// --- 第三阶段：进入全速代理阶段 ---
	if pCtx.isProxying {
		// 此时后端已经通了，数据直接往管道里塞，relayUp 协程会负责搬运。
		msg, _ := c.Next(-1)
		if len(msg) > 0 {
			msgCopy := pool.Get()
			n := copy(msgCopy, msg)
			select {
			case pCtx.writeChan <- msgCopy[:n]:
			default:
				pool.Put(msgCopy)
				logger.Errorf("❌ [拥塞] 后端发送太慢，丢弃连接 (Client %s)", c.RemoteAddr())
				return gnet.Close
			}
		}
	}

	return gnet.None
}

/**
 * 📞 asyncDial：后台拨号助手
 * 这就像是在后勤部打个电话，通了之后把电话筒交给搬运工。
 */
func (s *Server) asyncDial(c gnet.Conn, ctx *connContext, rule config.RouteRule) {
	// 🔋 [连接池命中]：优先从温热的池子里拿链接，减少 30ms-100ms 的 TCP 握手开销！
	backendConn, err := s.pool.Acquire(rule)
	if err != nil {
		logger.Errorf("❌ [拨号失败] 无法连通 %s: %v", rule.Addr, err)
		c.Close()
		return
	}

	logger.Infof("✅ [连通确认] 后端 %s 已就绪", rule.Addr)

	ctx.backendConn = backendConn
	ctx.isDialing = false
	ctx.isProxying = true

	// 开启“双向奔赴”的透明传输：
	// 1. RelayBack：后端回复的数据 -> 异步转发给客户端
	go s.transport.RelayBack(c, backendConn)
	// 2. relayUp：管道里的客户端数据 -> 写入后端
	go s.relayUp(c, ctx)

	// 📣 唤醒 gnet：告诉负责这个连接的线程，“数据已经可以发啦，快去同步一下状态！”
	c.Wake(nil)
}

/**
 * 🚚 relayUp：上行搬运工
 * 它一直守在 ctx.writeChan 管道口，只要里面有客户端传来的包，它就立马发给后端。
 */
func (s *Server) relayUp(c gnet.Conn, ctx *connContext) {
	// 善后处理：当连接关闭时，清理掉管道里还没发完的垃圾，归还内存给系统。
	defer func() {
		for msg := range ctx.writeChan {
			pool.Put(msg)
		}
	}()

	for msg := range ctx.writeChan {
		_, err := ctx.backendConn.Write(msg)
		// 💡 [性能细节]：一从网卡发出去，立刻归还内存块给 sync.Pool 复用。
		pool.Put(msg)
		if err != nil {
			logger.Errorf("❌ [上行异常] %v", err)
			c.Close()
			return
		}
	}
}

// OnClose：曲终人散，当连接断开（不管是主动还是被动）时的资源清理现场。
func (s *Server) OnClose(c gnet.Conn, err error) gnet.Action {
	if err != nil {
		logger.Debugf("👋 [关闭] 连接异常终止: %v", err)
	}

	if c.Context() != nil {
		pCtx := c.Context().(*connContext)
		// 🛡️ 这种“一人做事一人当”的设计确保后端连接也被妥善关闭，不会造成内存泄漏。
		pCtx.closeOnce.Do(func() {
			if pCtx.backendConn != nil {
				pCtx.backendConn.Close()
			}
			if pCtx.writeChan != nil {
				close(pCtx.writeChan) // 通知 relayUp 协程收工
			}
		})
	}
	return gnet.None
}

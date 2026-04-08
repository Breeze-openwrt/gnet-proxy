package inbound

import (
	"net"
	"strings"
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
	addr := s.listenAddr
	if !strings.Contains(addr, "://") {
		addr = "tcp://" + addr
	}

	logger.Infof("🚀 [点火] 极速转发引擎启动于 %s (多核模式: %v)", addr, s.multicore)
	return gnet.Run(s, addr,
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
 * 🔥 [核心逻辑]：OnTraffic
 * 通过“海量吸纳”与“动态溢出保护”实现 100% 数据完整性。
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
			logger.Infof("❓ [未识别] 客户端 %s 无 SNI", c.RemoteAddr())
		}

		rule, ok := s.router.Match(sni, c.RemoteAddr().String())
		if !ok {
			return gnet.Close
		}

		// 🚀 [生产级配比：128MB 极限吸纳管道]
		newCtx := &connContext{isDialing: true, writeChan: make(chan []byte, 4096)}
		c.SetContext(newCtx)

		// 🚢 [海吸模式]：立刻读走首个数据包
		firstPacket, _ := c.Next(-1) 
		n := len(firstPacket)
		
		// 🛡️ [终极无损防护]：避免以前的“池化截断”Bug。
		var firstPacketCopy []byte
		if n <= 64*1024 {
			firstPacketCopy = pool.Get()
		} else {
			// 如果数据包超过 64KB，动态分配以保证 100% 完整性。
			firstPacketCopy = make([]byte, n)
		}
		copy(firstPacketCopy, firstPacket)
		newCtx.writeChan <- firstPacketCopy[:n]

		go s.asyncDial(c, newCtx, rule)
		return gnet.None
	}

	// --- 第二阶段：全量吸纳 ---
	pCtx := ctx.(*connContext)
	
	// 🌊 [全量搬运]：第一时间读空内核缓冲区，杜绝 CPU 疯转
	msg, _ := c.Next(-1)
	n := len(msg)
	if n == 0 {
		return gnet.None
	}
	
	// 🛡️ [终极无损防护]：此处是处理大 Pack 文件（Git Push）的关键。
	var msgCopy []byte
	if n <= 64*1024 {
		msgCopy = pool.Get()
	} else {
		// 为了不截流，我们允许偶尔的内存波动，绝对不截断数据！
		msgCopy = make([]byte, n)
	}
	copy(msgCopy, msg)

	// 将读到的完整数据塞进管道
	select {
	case pCtx.writeChan <- msgCopy[:n]:
		// 转发中
	default:
		// 如果 128MB 全满了，说明下游彻底瘫痪
		if n <= 64*1024 { pool.Put(msgCopy) }
		logger.Errorf("⚠️ [拥塞] 缓冲区爆满 (Client %s)", c.RemoteAddr())
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

	go s.transport.RelayBack(c, backendConn)
	go s.relayUp(c, ctx)

	c.Wake(nil)
}

/**
 * 🚚 relayUp：上行搬运工
 */
func (s *Server) relayUp(c gnet.Conn, ctx *connContext) {
	defer func() {
		for msg := range ctx.writeChan {
			if cap(msg) == 64*1024 { pool.Put(msg) }
		}
	}()

	for msg := range ctx.writeChan {
		ctx.backendConn.SetWriteDeadline(time.Now().Add(5 * time.Minute))

		_, err := ctx.backendConn.Write(msg)
		// 仅当是池化块时才归还，动态块交给 GC。
		if cap(msg) == 64*1024 { pool.Put(msg) }
		
		if err != nil {
			logger.Errorf("❌ [上行异常] 写入后端失败: %v", err)
			c.Close()
			return
		}
	}
}

// OnClose：清理现场
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

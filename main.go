package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
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
	addr          string
	multicore     bool
	proxyProtocol bool
	verbosity     int
	routes        map[string]RouteRule
	bufferPool    sync.Pool
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

func (s *proxyServer) OnBoot(eng gnet.Engine) gnet.Action {
	s.infof("🚀 gnet-proxy 极速分流器启动成功！监听: %s", s.addr)
	s.infof("🧠 多核模式: %v | 日志冗余级别: %d", s.multicore, s.verbosity)
	return gnet.None
}

// 🔌 新连接接入时触发
func (s *proxyServer) OnOpen(c gnet.Conn) (out []byte, action gnet.Action) {
	s.infof("🔌 [接入] 新客户端: %s", c.RemoteAddr())
	return nil, gnet.None
}

func buildProxyHeader(clientAddr, serverAddr net.Addr) string {
	cHost, cPort, _ := net.SplitHostPort(clientAddr.String())
	sHost, sPort, _ := net.SplitHostPort(serverAddr.String())
	if strings.Contains(cHost, ":") {
		return fmt.Sprintf("PROXY TCP6 %s %s %s %s\r\n", cHost, sHost, cPort, sPort)
	}
	return fmt.Sprintf("PROXY TCP4 %s %s %s %s\r\n", cHost, sHost, cPort, sPort)
}

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

func (s *proxyServer) OnTraffic(c gnet.Conn) gnet.Action {
	ctx := c.Context()
	if ctx == nil {
		// ⚠️ 核心修复：这里不能写死 Peek(1024)！因为典型的 TLS ClientHello 通常只有 512 字节左右。
		// 在 gnet v2 中，如果缓冲区数据不够 1024，可能会拒绝返回或抛出错误，导致整个解析流程卡死。
		// 使用 c.Peek(-1) 代表“把当前缓冲区里拥有的所有字节都借给我看一眼”。
		buf, _ := c.Peek(-1)

		// 如果读出来的报文连 5 个字节（TLS头）都没有，就不要麻烦解析器了，继续等
		if len(buf) < 5 {
			return gnet.None
		}

		sni, err := ParseSNI(buf)

		if err != nil {
			if err == ErrIncompletePacket {
				// 握手包还没接收完，继续包组装
				return gnet.None
			}
			s.infof("❓ [无域名/非TLS流量] 客户端 %s 的流量未识别到 SNI (原因: %v)，将尝试 Fallback 回退路由", c.RemoteAddr(), err)
		} else {
			s.infof("🔍 [SNI 提取成功] 客户端 %s 识别到域名: %s", c.RemoteAddr(), sni)
		}

		// 路由匹配优先级：精准命中 > 星号 (*) Fallback 兜底
		rule, ok := s.routes[sni]
		if !ok {
			// 如果没精准命中，或是根本没提取到 SNI，尝试找万能回退路由
			fallbackRule, fallbackOk := s.routes["*"]
			if !fallbackOk {
				// 没有配置回退，只能拒绝
				s.infof("⚠️ [拒绝访问] 域名 [%s] 未匹配且无 (*) 回退路由，掐断客户端 %s", sni, c.RemoteAddr())
				return gnet.Close
			}
			rule = fallbackRule
			s.infof("🛡️ [启用 Fallback] 客户端 %s 未完全匹配，路由至兜底后端: %s", c.RemoteAddr(), rule.Addr)
		} else {
			s.infof("🎯 [路由精准命中] 客户端 %s 分流: [%s] -> %s", c.RemoteAddr(), sni, rule.Addr)
		}

		backendConn, err := s.dialBackend(rule)
		if err != nil {
			s.errorf("❌ [拨号失败] 无法连接到后端 %s (客户端 %s): %v", rule.Addr, c.RemoteAddr(), err)
			return gnet.Close
		}
		s.infof("✅ [拨号成功] 已连通后端 %s (客户端 %s)", rule.Addr, c.RemoteAddr())

		shouldSendProxy := s.proxyProtocol
		if rule.ProxyProtocol != nil {
			// 如果用户显式配置了当前路由的 proxy_protocol，则强力覆盖全局配置
			shouldSendProxy = *rule.ProxyProtocol
		}

		if shouldSendProxy {
			proxyHeader := buildProxyHeader(c.RemoteAddr(), c.LocalAddr())
			backendConn.Write([]byte(proxyHeader))
			s.tracef("🛡️  发送 PROXY 报头 (Client: %s)", c.RemoteAddr())
		}
		newCtx := &connContext{backendConn: backendConn, isProxying: true}
		c.SetContext(newCtx)
		go s.proxyBack(c, backendConn)
	}

	pCtx := c.Context().(*connContext)
	msg, _ := c.Next(-1)

	// 记录请求转发量（只有在 trace 级别，也就是 -vvv 时才大规模刷屏显示字节数，避免性能损耗）
	s.tracef("⬆️ [上行数据] (Client %s -> Backend) 转发了 %d 字节", c.RemoteAddr(), len(msg))

	_, err := pCtx.backendConn.Write(msg)
	if err != nil {
		s.errorf("❌ [转发异常] 发送数据到后端失败 (Client %s): %v", c.RemoteAddr(), err)
		return gnet.Close
	}
	return gnet.None
}

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

func (s *proxyServer) OnClose(c gnet.Conn, err error) gnet.Action {
	if err != nil {
		s.errorf("❌ [连接断开] 客户端异常断开 (Client %s): %v", c.RemoteAddr(), err)
	} else {
		s.infof("👋 [连接关闭] 客户端正常断开 (Client %s)", c.RemoteAddr())
	}

	if c.Context() != nil {
		pCtx := c.Context().(*connContext)
		if pCtx.backendConn != nil {
			s.debugf("🧹 [清理] 销毁与后端的连接 (Client %s)", c.RemoteAddr())
			pCtx.backendConn.Close()
		}
	}
	return gnet.None
}

func daemonize() {
	newArgs := make([]string, 0)
	for _, arg := range os.Args[1:] {
		if arg != "-d" {
			newArgs = append(newArgs, arg)
		}
	}
	cmd := exec.Command(os.Args[0], newArgs...)
	cmd.Start()
	fmt.Printf("👻 [INFO] gnet-proxy 发起影子运行 (PID: %d)\n", cmd.Process.Pid)
	os.Exit(0)
}

// 🛡️ 无缝热重启机制：自动杀死上次忘了关的僵尸进程，保证端口不冲突
func enforceSingleton() {
	pidFile := filepath.Join(os.TempDir(), "gnet-proxy.pid")
	if data, err := os.ReadFile(pidFile); err == nil {
		if oldPid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if process, err := os.FindProcess(oldPid); err == nil {
				// 尝试向该进城发送关闭信号 (如果进程不存在，Kill 在某些系统下也会返回 nil，但这只是尝试)
				process.Kill()
				// 等待一小会儿确保端口被彻底释放
				time.Sleep(200 * time.Millisecond)
			}
		}
	}
	// 记录当次运行的新 PID
	currentPid := os.Getpid()
	os.WriteFile(pidFile, []byte(strconv.Itoa(currentPid)), 0644)
}

func main() {
	configPath := pflag.StringP("config", "c", "config.jsonc", "配置文件路径")
	isDaemon := pflag.BoolP("daemon", "d", false, "以影子守护进程模式运行")
	// 🚀 工业级技巧：原生支持 Count 类型参数，无需手动遍历 os.Args
	verbosityPtr := pflag.CountP("verbose", "v", "详细日志模式 (可叠加，例如 -v, -vv, -vvv)")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🚀 gnet-proxy 极速转发器引擎\n\n用法: %s [选项]\n\n核心选项:\n", os.Args[0])
		pflag.PrintDefaults()
	}
	pflag.Parse()

	// ================= 加载配置 =================
	config, err := LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Msgf("❌ 配置解析错误: %v", err)
	}

	verbosity := *verbosityPtr

	// 脱壳后台驻留执行
	if *isDaemon {
		daemonize()
	}

	// 此时代码已经在最终执行环境（要么是前台运行，要么是脱壳后的子进程），立刻触发单例猎杀
	enforceSingleton()

	// 🚦 如果命令行没有传 -v 参数，优先使用配置文件中的 log_level
	if verbosity == 0 {
		switch strings.ToLower(config.LogLevel) {
		case "info":
			verbosity = LogLevelInfo
		case "debug":
			verbosity = LogLevelDebug
		case "trace":
			verbosity = LogLevelTrace
		}
	}

	// 初始化底层 Zerolog 的阻断拦截级别
	zerolog.SetGlobalLevel(zerolog.Disabled)
	if verbosity >= LogLevelInfo {
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
	if verbosity >= LogLevelDebug {
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	}
	if verbosity >= LogLevelTrace {
		zerolog.SetGlobalLevel(zerolog.TraceLevel)
	}

	// 📋 智能日志分流器
	var logStream io.Writer = io.Discard
	if verbosity > LogLevelSilent {
		logStream = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}

	if config.LogFile != "" {
		// 追加写入模式打开日志文件
		f, perr := os.OpenFile(config.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		if perr == nil {
			// 在终端和文本文件双流并行输出
			log.Logger = zerolog.New(zerolog.MultiLevelWriter(logStream, f)).With().Timestamp().Logger()
		} else {
			// 如果没有权限创建日志文件，回滚到单流输出
			log.Logger = zerolog.New(logStream).With().Timestamp().Logger()
			log.Error().Msgf("❌ 无法打开日志文件 (%s): %v", config.LogFile, perr)
		}
	} else {
		log.Logger = zerolog.New(logStream).With().Timestamp().Logger()
	}

	p := &proxyServer{
		addr:          config.ListenAddr,
		multicore:     config.Multicore,
		proxyProtocol: config.ProxyProtocol,
		verbosity:     verbosity, // 保持冗余兼容性
		routes:        config.Routes,
		bufferPool: sync.Pool{
			New: func() interface{} { return make([]byte, 32*1024) },
		},
	}

	// 🛡️ [长连接保障] 显式告知 gnet 为所有客户端连接开启 TCP Keep-Alive 保活机制
	err = gnet.Run(p, "tcp://"+p.addr,
		gnet.WithMulticore(p.multicore),
		gnet.WithTCPKeepAlive(5*time.Minute),
	)
	if err != nil {
		log.Fatal().Msgf("❌ 运行失败: %v", err)
	}
}

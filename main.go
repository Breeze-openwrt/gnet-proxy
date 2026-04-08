package main

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
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
	if s.verbosity >= LogLevelTrace { log.Trace().Msgf(format, v...) }
}

func (s *proxyServer) debugf(format string, v ...interface{}) {
	if s.verbosity >= LogLevelDebug { log.Debug().Msgf(format, v...) }
}

func (s *proxyServer) infof(format string, v ...interface{}) {
	if s.verbosity >= LogLevelInfo { log.Info().Msgf(format, v...) }
}

func (s *proxyServer) errorf(format string, v ...interface{}) {
	if s.verbosity >= LogLevelInfo { log.Error().Msgf(format, v...) }
}

func (s *proxyServer) OnBoot(eng gnet.Engine) gnet.Action {
	s.infof("🚀 gnet-proxy 极速分流器启动成功！监听: %s", s.addr)
	s.infof("🧠 多核模式: %v | 日志冗余级别: %d", s.multicore, s.verbosity)
	return gnet.None
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
	return net.DialTimeout(network, target, 3*time.Second)
}

func (s *proxyServer) OnTraffic(c gnet.Conn) gnet.Action {
	ctx := c.Context()
	if ctx == nil {
		buf, _ := c.Peek(1024) 
		sni, err := ParseSNI(buf)
		if err != nil {
			if err == ErrIncompletePacket { return gnet.None }
			return gnet.Close 
		}
		rule, ok := s.routes[sni]
		if !ok {
			s.debugf("🔎 域名识别成功 [%s] -> 未配置路由逻辑", sni)
			return gnet.Close
		}
		s.infof("🎯 域名命中 [%s] -> %s", sni, rule.Addr)

		backendConn, err := s.dialBackend(rule)
		if err != nil {
			s.errorf("❌ 后端拨号失败 %s: %v", rule.Addr, err)
			return gnet.Close
		}

		shouldSendProxy := s.proxyProtocol 
		if strings.Contains(fmt.Sprintf("%v", rule), "proxy_protocol") {
			shouldSendProxy = rule.ProxyProtocol
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
	_, err := pCtx.backendConn.Write(msg)
	if err != nil { return gnet.Close }
	return gnet.None
}

func (s *proxyServer) proxyBack(c gnet.Conn, backend net.Conn) {
	defer backend.Close()
	buf := s.bufferPool.Get().([]byte)
	defer s.bufferPool.Put(buf)
	for {
		n, err := backend.Read(buf)
		if err != nil { break }
		err = c.AsyncWrite(buf[:n], nil)
		if err != nil { break }
	}
	c.Wake(nil) 
}

func (s *proxyServer) OnClose(c gnet.Conn, err error) gnet.Action {
	if c.Context() != nil {
		pCtx := c.Context().(*connContext)
		if pCtx.backendConn != nil { pCtx.backendConn.Close() }
	}
	return gnet.None
}

func daemonize() {
	newArgs := make([]string, 0)
	for _, arg := range os.Args[1:] {
		if arg != "-d" { newArgs = append(newArgs, arg) }
	}
	cmd := exec.Command(os.Args[0], newArgs...)
	cmd.Start()
	fmt.Printf("👻 [INFO] gnet-proxy 发起影子运行 (PID: %d)\n", cmd.Process.Pid)
	os.Exit(0)
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

	verbosity := *verbosityPtr
	if *isDaemon {
		daemonize()
	}

	// 🚦 初始化 zerolog 级别 (支持多级详细日志)
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

	config, err := LoadConfig(*configPath)
	if err != nil { log.Fatal().Msgf("❌ 配置解析错误: %v", err) }

	// 📋 配置日志输出管道
	var logStream io.Writer = io.Discard
	if verbosity > LogLevelSilent {
		logStream = zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: time.RFC3339}
	}

	if config.LogFile != "" {
		f, _ := os.OpenFile(config.LogFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
		// 如果有日志文件，交互模式下双流输出，非交互模式下只写 JSON 到文件 (高性能)
		if verbosity > LogLevelSilent {
			log.Logger = zerolog.New(zerolog.MultiLevelWriter(logStream, f)).With().Timestamp().Logger()
		} else {
			log.Logger = zerolog.New(f).With().Timestamp().Logger()
		}
	} else {
		log.Logger = zerolog.New(logStream).With().Timestamp().Logger()
	}

	p := &proxyServer{
		addr:      config.ListenAddr,
		multicore: config.Multicore,
		proxyProtocol: config.ProxyProtocol,
		verbosity:     verbosity, // 保持冗余兼容性
		routes:        config.Routes,
		bufferPool: sync.Pool{
			New: func() interface{} { return make([]byte, 32*1024) },
		},
	}

	err = gnet.Run(p, "tcp://"+p.addr, gnet.WithMulticore(p.multicore))
	if err != nil { log.Fatal().Msgf("❌ 运行失败: %v", err) }
}

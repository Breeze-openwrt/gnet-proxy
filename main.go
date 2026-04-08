package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/panjf2000/gnet/v2"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"
)

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
		addr:      config.ListenAddr,
		multicore: config.Multicore,
		verbosity: verbosity, // 保持冗余兼容性
		routes:    config.Routes,
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

package main

import (
	"fmt"
	"os"

	"github.com/rs/zerolog/log"
	"github.com/spf13/pflag"

	"gnet-proxy/pkg/common/daemon"
	"gnet-proxy/pkg/common/logger"
	"gnet-proxy/pkg/config"
	"gnet-proxy/pkg/core"
	"gnet-proxy/pkg/inbound"
	"gnet-proxy/pkg/outbound"
)

func main() {
	configPath := pflag.StringP("config", "c", "config.jsonc", "配置文件路径")
	isDaemon := pflag.BoolP("daemon", "d", false, "以影子守护进程模式运行")
	verbosityPtr := pflag.CountP("verbose", "v", "详细日志模式 (可叠加，例如 -v, -vv, -vvv)")

	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🚀 gnet-proxy 极速转发器引擎 (DDD架构版)\n\n用法: %s [选项]\n\n核心选项:\n", os.Args[0])
		pflag.PrintDefaults()
	}
	pflag.Parse()

	// ================= 0. 系统安装器指令 =================
	if len(os.Args) > 1 && os.Args[1] == "install" {
		daemon.Install()
		return
	}

	// ================= 1. 加载域配置 =================
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		log.Fatal().Msgf("❌ 配置解析错误: %v", err)
	}

	verbosity := *verbosityPtr

	// ================= 2. 后台隔离 (守护进程装配) =================
	if *isDaemon {
		daemon.Daemonize()
	}
	// 驻留或前台启动时检测进程死锁
	daemon.EnforceSingleton()

	// ================= 3. 基础设施装配 (Logger) =================
	if verbosity == 0 {
		switch cfg.LogLevel {
		case "info":
			verbosity = logger.LevelInfo
		case "debug":
			verbosity = logger.LevelDebug
		case "trace":
			verbosity = logger.LevelTrace
		}
	}
	logger.Setup(verbosity, cfg.LogFile)

	// ================= 4. 业务域装配 (DI依赖注入) =================
	// Router Core
	router := core.NewRouter(cfg.Routes)

	// Outbound Copmponents
	dialer := outbound.NewDialer()
	transport := outbound.NewTransport()

	// Inbound Component (装填出站模块与路由核心)
	server := inbound.NewServer(cfg.ListenAddr, cfg.Multicore, router, dialer, transport)

	// ================= 5. 点火发射 =================
	if err := server.Run(); err != nil {
		logger.Errorf("❌ 运行失败: %v", err)
	}
}

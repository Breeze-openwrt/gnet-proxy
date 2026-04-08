package main

import (
	"fmt" // 基础格式化输出工具包，用于在终端打印文字
	"os"  // 操作系统接口工具包，用于处理命令行参数、文件路径和退出程序

	"github.com/rs/zerolog/log" // 高性能日志库，这里用于输出系统级严重错误
	"github.com/spf13/pflag"    // 增强版的命令行参数解析库，支持更复杂的长短参数（如 -c 或 --config）

	"gnet-proxy/pkg/common/daemon"  // 自定义包：处理守护进程逻辑，让程序在后台悄悄运行
	"gnet-proxy/pkg/common/logger"  // 自定义包：封装了日志系统，控制日志的显示风格和详细程度
	"gnet-proxy/pkg/config"         // 自定义包：负责读取和解析 JSONC 配置文件
	"gnet-proxy/pkg/core"           // 自定义包：核心业务逻辑，包含最关键的路由分配算法
	"gnet-proxy/pkg/inbound"        // 自定义包：入站引擎，负责在大门口（端口）接收网络流量
	"gnet-proxy/pkg/outbound"       // 自定义包：出站/转发引擎，负责把流量送往指定目的地
)

/**
 * 💡 [源码阅读引导]：
 * 这里的 main 函数是整个程序的“点火钥匙”。
 * 它的工作很简单：1. 听命令（解析参数） 2. 搬零件（实例化各种对象） 3. 组装零件（依赖注入） 4. 启动机器（运行服务器）。
 */
func main() {
	// 🛠️ [定义命令行参数]
	// 定义一个名为 config 的参数，默认值是 config.jsonc，缩写是 -c
	configPath := pflag.StringP("config", "c", "config.jsonc", "配置文件路径")
	// 定义一个控制程序是否在后台运行的标志位，缩写是 -d
	isDaemon := pflag.BoolP("daemon", "d", false, "以影子守护进程模式运行")
	// 定义日志详细程度，-v 越多，日志出来的越详细
	verbosityPtr := pflag.CountP("verbose", "v", "详细日志模式 (可叠加，例如 -v, -vv, -vvv)")

	// 设置当用户输入 --help 或输入错误命令时的提示信息
	pflag.Usage = func() {
		fmt.Fprintf(os.Stderr, "🚀 gnet-proxy 极速转发器引擎 (DDD架构版)\n\n用法: %s [选项]\n\n核心选项:\n", os.Args[0])
		pflag.PrintDefaults() // 打印上面定义好的参数说明
	}
	pflag.Parse() // 🏁 正式解析这些参数，从此以后我们就能拿到用户输入的值了

	// ================= 0. 系统安装器指令 (系统级集成) =================
	// 就像安装软件一样，这个逻辑负责把程序注册到操作系统的服务里
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "install":
			daemon.Install() // 并在系统里安装成服务（Windows Service）
			return           // 安装完直接退出
		case "uninstall":
			daemon.Uninstall() // 从系统里卸载服务
			return             // 卸载完直接退出
		}
	}

	// ================= 1. 加载域配置 (读取说明书) =================
	// 我们告诉程序：“去读这个配置文件，告诉我该往哪转发流量”。
	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		// 如果配置文件写错了（比如少个逗号），程序会在这里报错并直接“罢工”
		log.Fatal().Msgf("❌ 配置解析错误: %v", err)
	}

	verbosity := *verbosityPtr

	// ================= 2. 后台隔离 (守护进程装配) =================
	// 所谓“守护进程”，就是把程序藏在背后。就算你关掉控制台窗口，它也能继续跑。
	if *isDaemon {
		daemon.Daemonize() // 这里的逻辑会创建一个“影子进程”来接手工作
	}
	// 🛡️ [防双开保护]：通过文件锁确保同一台服务器上只跑一个 gnet-proxy，防止端口冲突。
	daemon.EnforceSingleton()

	// ================= 3. 基础设施装配 (Logger 日志仪表盘) =================
	// 这里根据配置文件和指令参数，决定日志输出内容的多少。
	// Info 级别能让我们看到“谁来了”，Debug 级别能让我们看到“它去哪了”，Trace 则更细。
	if cfg.Log.Disabled {
		verbosity = logger.LevelSilent // 彻底静音，性能最高
	} else if verbosity == 0 {
		// 如果用户没在命令行输入 -v，我们就按配置文件里的设置来
		switch cfg.Log.Level {
		case "info":
			verbosity = logger.LevelInfo
		case "debug":
			verbosity = logger.LevelDebug
		case "trace":
			verbosity = logger.LevelTrace
		case "warn", "error":
			verbosity = logger.LevelInfo // 保底设置为 Info，不漏掉关键信息
		}
	}
	// 初始化日志系统，决定是打印到屏幕还是写到文件
	logger.Setup(verbosity, cfg.Log.Output, cfg.Log.Timestamp)

	// ================= 4. 业务域装配 (DI依赖注入 - 组装机器) =================
	/**
	 * 🏗️ [设计模式赏析]：这里采用了典型的“依赖注入 (Dependency Injection)”。
	 * 我们先把每一个功能模块（路由、拨号器、池、转发器）造好，然后像拼积木一样传给 Server。
	 * 这样写代码的好处是方便测试，每个零件都能拆下来单独检查。
	 */

	// 🧠 指挥部：实例化路由核心，它拿着各种匹配规则来指挥流量
	router := core.NewRouter(cfg.Routes)

	// 📞 外联部：拨号组件。它是“跑腿的”，负责去建立真实的 TCP 链接
	dialer := outbound.NewDialer()
	// 🔋 后勤部：连接池管理。
	// 这里的思想极具前瞻性：它会预先跟后端服务（如 Xray）练好链接，省去每次都现连接的时间。
	pool := outbound.NewConnectionPool(cfg, dialer)
	// 🚚 运输部：负责数据包在客户端和后端之间来回搬运的具体业务
	transport := outbound.NewTransport()

	// 🌡️ [启动预热]：趁现在还没流量进来，在后台静默填充初始连接池。
	// 就像餐馆开门前先把菜切好一样。
	pool.PreheatAll()

	// 🚪 前台接待：装配入站服务。它依赖上面所有的“部门”。
	server := inbound.NewServer(cfg.ListenAddr, cfg.Multicore, router, dialer, pool, transport)

	// ================= 5. 点火发射 (真正的死循环开始) =================
	// 一旦调用 Run()，程序就会进入高效率的 I/O 多路复用循环中，开始 24x7 不间断服务。
	if err := server.Run(); err != nil {
		logger.Errorf("❌ 运行失败: %v", err)
	}
}

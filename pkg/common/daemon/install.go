package daemon

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Install 尝试将当前程序配置为 systemd 自启服务
func Install() {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ [安装失败] 仅支持 Linux/Debian 环境下使用 systemd 安装。")
		os.Exit(1)
	}

	fmt.Println("🚀 正在安装 gnet-proxy 服务至 Debian Systemd...")

	// 1. 获取当前二进制路径并移动到系统标准位置
	currentExe, err := os.Executable()
	if err != nil {
		fmt.Printf("❌ 获取程序路径失败: %v\n", err)
		os.Exit(1)
	}

	binPath := "/usr/local/bin/gnet-proxy"
	if currentExe != binPath {
		if err := copyFile(currentExe, binPath); err != nil {
			fmt.Printf("❌ 无法复制可执行文件到 %s: %v\n(请使用 sudo 提权运行)\n", binPath, err)
			os.Exit(1)
		}
		os.Chmod(binPath, 0755)
	}

	// 2. 创建并复制默认配置文件
	configDir := "/etc/gnet-proxy"
	configPath := filepath.Join(configDir, "config.jsonc")
	os.MkdirAll(configDir, 0755)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultConfig := `{
  "listen_addr": "[::]:4488",
  "multicore": true,
  "log": {
    "disabled": false,
    "level": "info",
    "output": "/var/log/gnet-proxy.log",
    "timestamp": true
  },
  "routes": {
    "updates.cdn-apple.com": {
      "addr": "tcp://127.0.0.1:443"
    },
    "*": {
      "addr": "tcp://127.0.0.1:4488"
    }
  }
}`
		os.WriteFile(configPath, []byte(defaultConfig), 0644)
		fmt.Printf("📝 已生成默认配置文件: %s\n", configPath)
	}

	// 3. 生成 systemd .service 脚本
	// 兼容 -d 模式：配置为 forking 类型，让 systemd 根据 PID 文件追踪后台进程
	pidFile := filepath.Join(os.TempDir(), "gnet-proxy.pid")
	serviceContent := fmt.Sprintf(`[Unit]
Description=Gnet-Proxy High Performance XTLS Reverse Proxy
After=network.target

[Service]
Type=forking
PIDFile=%s
ExecStart=%s -c %s -d
Restart=always
RestartSec=5
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
`, pidFile, binPath, configPath)

	servicePath := "/etc/systemd/system/gnet-proxy.service"
	if err := os.WriteFile(servicePath, []byte(serviceContent), 0644); err != nil {
		fmt.Printf("❌ 写入 systemd 配置失败: %v\n", err)
		os.Exit(1)
	}

	// 4. 加载并启动服务
	fmt.Println("⚙️ 正在重新加载 systemd 守护进程记录...")
	exec.Command("systemctl", "daemon-reload").Run()
	exec.Command("systemctl", "enable", "gnet-proxy").Run()

	// 强制终止旧有进程以防止端口冲突
	exec.Command("systemctl", "stop", "gnet-proxy").Run()

	if err := exec.Command("systemctl", "start", "gnet-proxy").Run(); err != nil {
		fmt.Printf("❌ 服务启动异常: %v\n", err)
	} else {
		fmt.Println("✅ [完美安装] gnet-proxy 已经作为开机驻留服务成功安装在幕后运行了！")
		fmt.Println("💡 查看运行状态: systemctl status gnet-proxy")
		fmt.Println("💡 实时查看日志: tail -f /var/log/gnet-proxy.log (或系统日志 journalctl -u gnet-proxy -f)")
	}
	os.Exit(0)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

// Uninstall 尝试注销并卸载本机在运行后台的 gnet-proxy 组件
func Uninstall() {
	if runtime.GOOS != "linux" {
		fmt.Println("❌ [卸载失败] 仅支持 Linux/Debian 环境下使用 systemd 安装的服务。")
		os.Exit(1)
	}

	fmt.Println("🗑️ 正在触发 gnet-proxy 工业级核平卸载程序...")

	// 1. 强力停机并注销自启守护
	fmt.Println("🛑 正在斩断后台幽灵连接 (Stop & Disable)")
	exec.Command("systemctl", "stop", "gnet-proxy").Run()
	exec.Command("systemctl", "disable", "gnet-proxy").Run()

	// 2. 清理系统注册单 (Service)
	servicePath := "/etc/systemd/system/gnet-proxy.service"
	if _, err := os.Stat(servicePath); err == nil {
		os.Remove(servicePath)
		fmt.Println("🧹 已抹除底层系统服务挂载记录 (systemd .service)")
	}

	// 重新归档 systemd 树
	exec.Command("systemctl", "daemon-reload").Run()

	// 3. 删除运行实体本身
	binPath := "/usr/local/bin/gnet-proxy"
	if _, err := os.Stat(binPath); err == nil {
		os.Remove(binPath)
		fmt.Println("🧹 已删除投放在 /usr/local/bin 内的可执行克隆体")
	}

	fmt.Println("✅ [功成身退] gnet-proxy 系统级服务已被连根拔起！")

	// ⚠️ 极其核心的操作警示：安全留痕
	fmt.Println("⚠️ [提示] 为了防止数据丢失，我们手下留情，为您保留了相关的私有文件：")
	fmt.Println("   📝 配置文件夹: /etc/gnet-proxy/")
	fmt.Println("   📓 日志流水册: /var/log/gnet-proxy.log")
	fmt.Println("   如果您希望彻彻底底骨灰级清理，请手动执行：rm -rf /etc/gnet-proxy /var/log/gnet-proxy.log")

	os.Exit(0)
}

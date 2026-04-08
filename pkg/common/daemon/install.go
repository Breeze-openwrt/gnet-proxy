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
  "log_level": "info",
  "log_file": "/var/log/gnet-proxy.log",
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

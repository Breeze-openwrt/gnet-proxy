package daemon

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Daemonize 将当前进程脱离控制台在后台运行 (Linux 专属优化版)
func Daemonize() {
	newArgs := make([]string, 0)
	for _, arg := range os.Args[1:] {
		if arg != "-d" {
			newArgs = append(newArgs, arg)
		}
	}
	cmd := exec.Command(os.Args[0], newArgs...)
	
	// 完全脱离终端，防止父进程退出时杀掉子进程
	cmd.Stdout = nil
	cmd.Stderr = nil
	
	err := cmd.Start()
	if err != nil {
		fmt.Printf("❌ [ERROR] 影子运行失败: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("👻 [INFO] gnet-proxy 已转入后台影子模式运行 (PID: %d)\n", cmd.Process.Pid)
	
	// 记录最新的 PID 到标准位置
	writePid(cmd.Process.Pid)
	
	os.Exit(0)
}

// EnforceSingleton 自动检测并释放先前残留的端口占用 (Linux 工业级)
func EnforceSingleton() {
	pidFile := getPidPath()
	data, err := os.ReadFile(pidFile)
	if err != nil {
		// 如果记录不存在，则记录当次 PID 并继续
		writePid(os.Getpid())
		return 
	}

	oldPid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || oldPid == os.Getpid() {
		writePid(os.Getpid())
		return
	}

	// 🕵️‍♂️ [自愈探测]：检查旧进程是否真的还在
	// 在 Go 中，FindProcess 在 Linux 下总是会“找到”，只有发送信号才能判断真假
	if process, err := os.FindProcess(oldPid); err == nil {
		// 为了在 Windows 下也能通过交叉编译，我们不直接引用 syscall.Signal(0)
		// 而是直接执行 Kill。如果进程已经不在了，Kill 会失败，我们直接忽略即可。
		fmt.Printf("🧹 [CLEAN] 发现旧进程 (PID: %d)，正在强力回收资源...\n", oldPid)
		_ = process.Kill() 
		
		// ⌛ 等待 500ms 确保 Linux 内核能彻底回收绑定的 443/TCP 端口
		time.Sleep(500 * time.Millisecond)
	}

	// 占领位置
	writePid(os.Getpid())
}

func getPidPath() string {
	// 🛰️ [Linux 工业标准路径]
	const standardPath = "/run/gnet-proxy.pid"
	
	// 探测是否有权操作 /run (通常需要 root)
	if f, err := os.OpenFile(standardPath, os.O_WRONLY|os.O_CREATE, 0644); err == nil {
		f.Close()
		return standardPath
	}
	
	// 备选路径：回退到临时目录（例如 /tmp/gnet-proxy.pid）
	return filepath.Join(os.TempDir(), "gnet-proxy.pid")
}

func writePid(pid int) {
	_ = os.WriteFile(getPidPath(), []byte(strconv.Itoa(pid)), 0644)
}

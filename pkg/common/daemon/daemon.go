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

// Daemonize 将当前进程脱离控制台在后台运行
func Daemonize() {
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

// EnforceSingleton 自动检测并杀死先前残留的进程，避免端口占用
func EnforceSingleton() {
	pidFile := filepath.Join(os.TempDir(), "gnet-proxy.pid")
	if data, err := os.ReadFile(pidFile); err == nil {
		if oldPid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			if process, err := os.FindProcess(oldPid); err == nil {
				// 尝试向该进程发送关闭信号
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

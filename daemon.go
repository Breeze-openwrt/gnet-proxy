package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

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

//go:build !windows && !linux

package engine

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// macOS 没有 Linux 的 Pdeathsig 和 Windows 的 Job Object：内核被强制结束时 syncthing 可能成为孤儿进程
// （正常退出、Tauri 退出关闭 stdin 都会先优雅关闭 syncthing）。这里在 home 下记录 syncthing 的 PID，
// 下次启动前若发现它仍在运行、已被 launchd 收养（父进程为 1）且确实是同名 syncthing，则先结束它，
// 避免旧进程占着数据目录锁和 22000 端口导致新实例反复启动失败。
func hideWindow(cmd *exec.Cmd) {}
func attachJob(cmd *exec.Cmd)  {}

func recordChild(home string, pid int) {
	_ = os.WriteFile(filepath.Join(home, pidFileName), []byte(strconv.Itoa(pid)), 0o600)
}

func reapStale(home, binary string) {
	b, err := os.ReadFile(filepath.Join(home, pidFileName))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 1 || pid == os.Getpid() {
		return
	}
	if syscall.Kill(pid, 0) != nil {
		return
	}
	out, err := exec.Command("ps", "-o", "ppid=,comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || !isOrphanOf(string(out), binary) {
		log.Printf("上次记录的 syncthing PID %d 仍在运行，但不是本程序残留的孤儿进程（%q），不处理", pid, strings.TrimSpace(string(out)))
		return
	}
	log.Printf("结束上次残留的 syncthing 孤儿进程 PID %d", pid)
	_ = syscall.Kill(pid, syscall.SIGTERM)
	for i := 0; i < 50; i++ {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
}

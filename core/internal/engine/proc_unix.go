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
// 下次启动前结束仍在运行的旧进程（记录的监控进程及其工作进程，或已被 launchd 收养的同一程序、同一数据目录的 syncthing），
// 避免旧进程占着数据目录锁和 22000 端口导致新实例反复启动失败。
func hideWindow(cmd *exec.Cmd) {}
func attachJob(cmd *exec.Cmd)  {}

func recordChild(home string, pid int) {
	_ = os.WriteFile(filepath.Join(home, pidFileName), []byte(strconv.Itoa(pid)), 0o600)
}

func reapStale(home, binary string) {
	rec := 0
	if b, err := os.ReadFile(filepath.Join(home, pidFileName)); err == nil {
		rec, _ = strconv.Atoi(strings.TrimSpace(string(b)))
	}
	out, err := exec.Command("ps", "-axww", "-o", "pid=,ppid=,command=").Output()
	if err != nil {
		return
	}
	pids := staleTargets(string(out), binary, home, rec, os.Getpid())
	if len(pids) == 0 {
		return
	}
	log.Printf("结束上次残留的 syncthing 进程 %v", pids)
	for _, p := range pids {
		_ = syscall.Kill(p, syscall.SIGTERM)
	}
	for i := 0; i < 50; i++ {
		alive := false
		for _, p := range pids {
			if syscall.Kill(p, 0) == nil {
				alive = true
			}
		}
		if !alive {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, p := range pids {
		_ = syscall.Kill(p, syscall.SIGKILL)
	}
	time.Sleep(200 * time.Millisecond)
}

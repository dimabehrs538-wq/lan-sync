//go:build linux

package engine

import (
	"os/exec"
	"syscall"
)

// Linux 下内核被强杀时，syncthing 收到 SIGTERM 一起退出。
func hideWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM} }
func attachJob(cmd *exec.Cmd)  {}

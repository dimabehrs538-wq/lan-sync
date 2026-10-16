//go:build !windows

package engine

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
func attachJob(cmd *exec.Cmd)  {}

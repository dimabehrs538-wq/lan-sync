//go:build windows

package engine

import (
	"os/exec"
	"syscall"
	"unsafe"
)

const createNoWindow = 0x08000000

func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

var (
	kernel32                     = syscall.NewLazyDLL("kernel32.dll")
	procCreateJobObjectW         = kernel32.NewProc("CreateJobObjectW")
	procSetInformationJobObject  = kernel32.NewProc("SetInformationJobObject")
	procAssignProcessToJobObject = kernel32.NewProc("AssignProcessToJobObject")
	procOpenProcess              = kernel32.NewProc("OpenProcess")
	job                          uintptr
)

type jobBasicLimit struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}
type ioCounters struct{ a, b, c, d, e, f uint64 }
type jobExtLimit struct {
	Basic                 jobBasicLimit
	IoInfo                ioCounters
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryUsed uintptr
	PeakJobMemoryUsed     uintptr
}

// attachJob 把子进程放入 KILL_ON_JOB_CLOSE 的 Job Object，内核退出时 syncthing 一起结束。
func attachJob(cmd *exec.Cmd) {
	if job == 0 {
		h, _, _ := procCreateJobObjectW.Call(0, 0)
		if h == 0 {
			return
		}
		info := jobExtLimit{}
		info.Basic.LimitFlags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		procSetInformationJobObject.Call(h, 9, uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info))
		job = h
	}
	ph, _, _ := procOpenProcess.Call(0x0001|0x0100, 0, uintptr(cmd.Process.Pid)) // TERMINATE|SET_QUOTA
	if ph != 0 {
		procAssignProcessToJobObject.Call(job, ph)
		syscall.CloseHandle(syscall.Handle(ph))
	}
}

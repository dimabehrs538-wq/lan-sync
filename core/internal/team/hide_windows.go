package team

import "syscall"

func hideMarker(p string) {
	if u, err := syscall.UTF16PtrFromString(p); err == nil {
		_ = syscall.SetFileAttributes(u, syscall.FILE_ATTRIBUTE_HIDDEN)
	}
}

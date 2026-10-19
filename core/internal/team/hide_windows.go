package team

import "syscall"

func hideMarker(p string) {
	if u, err := syscall.UTF16PtrFromString(p); err == nil {
		_ = syscall.SetFileAttributes(u, syscall.FILE_ATTRIBUTE_HIDDEN)
	}
}

// unhideMarker 清除隐藏属性：Windows 上无法以截断方式覆盖已隐藏的文件。
func unhideMarker(p string) {
	if u, err := syscall.UTF16PtrFromString(p); err == nil {
		_ = syscall.SetFileAttributes(u, syscall.FILE_ATTRIBUTE_NORMAL)
	}
}

package team

import "syscall"

// longPath 把 Windows 8.3 短路径展开为长路径（路径必须存在；失败时原样返回）。
func longPath(p string) string {
	src, err := syscall.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	buf := make([]uint16, 512)
	for {
		n, err := syscall.GetLongPathName(src, &buf[0], uint32(len(buf)))
		if err != nil || n == 0 {
			return p
		}
		if int(n) <= len(buf) {
			return syscall.UTF16ToString(buf[:n])
		}
		buf = make([]uint16, n)
	}
}

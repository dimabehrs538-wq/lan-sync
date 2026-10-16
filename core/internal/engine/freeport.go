package engine

import "net"

// FreePort 返回本机 127.0.0.1 上一个空闲 TCP 端口。
func FreePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

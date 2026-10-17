// Package rotlog 提供按大小轮换的日志文件写入器，保证日志占用磁盘有上限：
// 当前文件超过 MaxSize 时重命名为 .1（旧的 .1 -> .2 ...），最多保留 Keep 个旧文件。
package rotlog

import (
	"fmt"
	"os"
	"sync"
)

type Writer struct {
	Path    string
	MaxSize int64
	Keep    int

	mu   sync.Mutex
	f    *os.File
	size int64
}

// Open 打开（追加）日志文件；maxSize<=0 时默认 5 MiB，keep<0 时默认 1。
func Open(path string, maxSize int64, keep int) (*Writer, error) {
	if maxSize <= 0 {
		maxSize = 5 << 20
	}
	if keep < 0 {
		keep = 1
	}
	w := &Writer{Path: path, MaxSize: maxSize, Keep: keep}
	if err := w.open(); err != nil {
		return nil, err
	}
	// 启动时已超限（例如旧版本留下的无限增长的日志）立即轮换
	if w.size >= w.MaxSize {
		if err := w.rotate(); err != nil {
			return nil, err
		}
	}
	return w, nil
}

func (w *Writer) open() error {
	f, err := os.OpenFile(w.Path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.f, w.size = f, st.Size()
	return nil
}

func (w *Writer) rotate() error {
	if w.f != nil {
		w.f.Close()
		w.f = nil
	}
	if w.Keep == 0 {
		os.Remove(w.Path)
	} else {
		os.Remove(fmt.Sprintf("%s.%d", w.Path, w.Keep))
		for i := w.Keep - 1; i >= 1; i-- {
			os.Rename(fmt.Sprintf("%s.%d", w.Path, i), fmt.Sprintf("%s.%d", w.Path, i+1))
		}
		if err := os.Rename(w.Path, w.Path+".1"); err != nil && !os.IsNotExist(err) {
			// Windows 上文件被占用时改名可能失败：退化为截断，仍保证有上限
			os.Truncate(w.Path, 0)
		}
	}
	return w.open()
}

func (w *Writer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}
	if w.size > 0 && w.size+int64(len(p)) > w.MaxSize {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.f.Write(p)
	w.size += int64(n)
	return n, err
}

func (w *Writer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Close()
	w.f = nil
	return err
}

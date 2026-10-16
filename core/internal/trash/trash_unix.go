//go:build !windows

package trash

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"time"
)

// TrashDir 可在测试中覆盖。
var TrashDir = func() (string, error) {
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(h, ".Trash"), nil
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "Trash"), nil
	}
	return filepath.Join(h, ".local", "share", "Trash"), nil
}

func toTrash(path string) error {
	dir, err := TrashDir()
	if err != nil {
		return err
	}
	files := dir
	if runtime.GOOS != "darwin" {
		files = filepath.Join(dir, "files")
		if err := os.MkdirAll(filepath.Join(dir, "info"), 0o700); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(files, 0o700); err != nil {
		return err
	}
	base := filepath.Base(path)
	name := base
	for i := 2; ; i++ {
		if _, err := os.Lstat(filepath.Join(files, name)); os.IsNotExist(err) {
			break
		}
		name = fmt.Sprintf("%s.%d", base, i)
	}
	if runtime.GOOS != "darwin" {
		info := fmt.Sprintf("[Trash Info]\nPath=%s\nDeletionDate=%s\n", (&url.URL{Path: path}).EscapedPath(), time.Now().Format("2006-01-02T15:04:05"))
		if err := os.WriteFile(filepath.Join(dir, "info", name+".trashinfo"), []byte(info), 0o600); err != nil {
			return err
		}
	}
	if err := os.Rename(path, filepath.Join(files, name)); err != nil {
		os.Remove(filepath.Join(dir, "info", name+".trashinfo"))
		return err // 跨分区等情况，由调用方决定是否永久删除
	}
	return nil
}

var _ = errUnsupported

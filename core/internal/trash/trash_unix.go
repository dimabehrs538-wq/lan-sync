//go:build !windows

package trash

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

// SystemTrashCLI 为 macOS 15 起系统自带的 /usr/bin/trash：由系统按卷放入对应的废纸篓（支持外置磁盘、
// 访达中“放回原处”），并且不需要读取受隐私保护的 ~/.Trash。为空表示不用（测试中会置空）。
var SystemTrashCLI = func() string {
	if runtime.GOOS == "darwin" {
		if fi, err := os.Stat("/usr/bin/trash"); err == nil && !fi.IsDir() {
			return "/usr/bin/trash"
		}
	}
	return ""
}()

func systemTrash(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, SystemTrashCLI, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("移到废纸篓失败：%v %s", err, out)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		return errors.New("移到废纸篓失败：文件仍在原位置")
	}
	return nil
}

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
	if SystemTrashCLI != "" {
		if err := systemTrash(path); err == nil {
			return nil
		}
		// 失败时退回到直接移动到 ~/.Trash
	}
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
		_, err := os.Lstat(filepath.Join(files, name))
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			// 例如 macOS 上 ~/.Trash 受隐私保护无法访问：返回错误，由调用方决定是否永久删除（不能无限重试）
			return err
		}
		if i > 10000 {
			return errors.New("废纸篓中同名文件过多")
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

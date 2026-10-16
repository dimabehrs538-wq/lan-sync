// Package trash 把文件/目录移到系统回收站（Windows 回收站、freedesktop 废纸篓、macOS 废纸篓）。
package trash

import (
	"errors"
	"os"
	"path/filepath"
)

// Remove 尝试移到回收站；失败时若 allowPermanent 为 true 则永久删除。
// 返回 trashed=true 表示已进入回收站，可恢复。
func Remove(path string, allowPermanent bool) (trashed bool, err error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(abs); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := toTrash(abs); err == nil {
		return true, nil
	} else if !allowPermanent {
		return false, err
	}
	return false, os.RemoveAll(abs)
}

var errUnsupported = errors.New("当前系统不支持回收站")

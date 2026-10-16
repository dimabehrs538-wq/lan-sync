// Package conflict 扫描与处理 Syncthing 冲突副本（*.sync-conflict-YYYYMMDD-HHMMSS-DEVICE.ext）。
package conflict

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var re = regexp.MustCompile(`^(.*)\.sync-conflict-(\d{8})-(\d{6})-([A-Z0-9]{7})(\..*)?$`)

type Conflict struct {
	Path     string    `json:"path"`     // 冲突副本完整路径
	Original string    `json:"original"` // 原文件完整路径
	Time     time.Time `json:"time"`
	Device   string    `json:"device"` // 设备短 ID（7 位）
}

// Parse 解析文件名，返回原文件名、时间和设备短 ID。
func Parse(name string) (orig string, t time.Time, dev string, ok bool) {
	m := re.FindStringSubmatch(name)
	if m == nil {
		return "", time.Time{}, "", false
	}
	t, err := time.ParseInLocation("20060102150405", m[2]+m[3], time.Local)
	if err != nil {
		return "", time.Time{}, "", false
	}
	return m[1] + m[5], t, m[4], true
}

// Scan 递归扫描目录下所有冲突副本（跳过 .stversions / .stfolder）。
func Scan(root string) ([]Conflict, error) {
	var out []Conflict
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			n := d.Name()
			if p != root && (n == ".stversions" || n == ".stfolder") {
				return filepath.SkipDir
			}
			return nil
		}
		if orig, t, dev, ok := Parse(d.Name()); ok {
			out = append(out, Conflict{Path: p, Original: filepath.Join(filepath.Dir(p), orig), Time: t, Device: dev})
		}
		return nil
	})
	return out, err
}

// Resolve 处理冲突：keepConflict=true 用冲突副本覆盖原文件；否则删除冲突副本保留原文件。
// root 用于校验路径必须在文件夹内。
func Resolve(root, conflictPath string, keepConflict bool) error {
	absRoot, _ := filepath.Abs(root)
	abs, _ := filepath.Abs(conflictPath)
	if !strings.HasPrefix(abs, absRoot+string(filepath.Separator)) {
		return errors.New("路径不在团队文件夹内")
	}
	orig, _, _, ok := Parse(filepath.Base(abs))
	if !ok {
		return errors.New("不是冲突文件")
	}
	if keepConflict {
		return os.Rename(abs, filepath.Join(filepath.Dir(abs), orig))
	}
	return os.Remove(abs)
}

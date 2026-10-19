package team

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

// VersionView 是一个历史版本，File 为 .stversions 中该版本文件的绝对路径（用于“打开”/“在文件夹中显示”）。
type VersionView struct {
	VersionTime time.Time `json:"versionTime"`
	ModTime     time.Time `json:"modTime"`
	Size        int64     `json:"size"`
	File        string    `json:"file,omitempty"`
}

// VersionedFile 是有历史版本的文件。Deleted=当前文件夹中已不存在（被删除）。
type VersionedFile struct {
	Path     string        `json:"path"`
	Deleted  bool          `json:"deleted"`
	Latest   time.Time     `json:"latest"`
	Versions []VersionView `json:"versions"` // 最新在前
}

// versionsDir 返回版本存放目录（simple/staggered 默认为 文件夹/.stversions）。
func versionsDir(f stclient.Folder) string {
	if p := f.Versioning.Params["fsPath"]; p != "" {
		if filepath.IsAbs(p) {
			return p
		}
		return filepath.Join(f.Path, p)
	}
	return filepath.Join(f.Path, ".stversions")
}

// VersionFile 计算某个版本在 .stversions 中的文件名：name~YYYYMMDD-HHMMSS.ext（本地时间）。
func VersionFile(f stclient.Folder, rel string, vt time.Time) string {
	rel = filepath.FromSlash(rel)
	dir, base := filepath.Split(rel)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" { // 例如 ".env"
		stem, ext = base, ""
	}
	return filepath.Join(versionsDir(f), dir, stem+"~"+vt.Local().Format("20060102-150405")+ext)
}

// BuildVersionList 把 Syncthing 的版本表整理为界面使用的列表（按最近版本时间倒序）。
func BuildVersionList(f stclient.Folder, raw map[string][]stclient.FileVersion) []VersionedFile {
	out := make([]VersionedFile, 0, len(raw))
	for rel, vs := range raw {
		if len(vs) == 0 {
			continue
		}
		vf := VersionedFile{Path: rel}
		for _, v := range vs {
			vv := VersionView{VersionTime: v.VersionTime, ModTime: v.ModTime, Size: v.Size}
			if p := VersionFile(f, rel, v.VersionTime); fileExists(p) {
				vv.File = p
			}
			vf.Versions = append(vf.Versions, vv)
		}
		sort.Slice(vf.Versions, func(i, j int) bool { return vf.Versions[i].VersionTime.After(vf.Versions[j].VersionTime) })
		vf.Latest = vf.Versions[0].VersionTime
		_, err := os.Lstat(filepath.Join(f.Path, filepath.FromSlash(rel)))
		vf.Deleted = errors.Is(err, os.ErrNotExist)
		out = append(out, vf)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Latest.Equal(out[j].Latest) {
			return out[i].Latest.After(out[j].Latest)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

// ListVersions 返回文件夹的历史版本列表。
func (s *Service) ListVersions(ctx context.Context, id string) ([]VersionedFile, error) {
	f, err := s.C.Folder(ctx, id)
	if err != nil {
		return nil, err
	}
	raw, err := s.C.Versions(ctx, id)
	if err != nil {
		return nil, err
	}
	return BuildVersionList(f, raw), nil
}

var ErrNoVersion = errors.New("没有找到这个文件的历史版本（可能已超过保留期限，或该文件夹未开启删除保护）")

// RestoreVersion 恢复文件的指定版本；versionTime 为零时恢复最近的版本。
// 当前文件（如存在）会先被保存为一个新的历史版本。
func (s *Service) RestoreVersion(ctx context.Context, id, rel string, versionTime time.Time) (time.Time, error) {
	raw, err := s.C.Versions(ctx, id)
	if err != nil {
		return time.Time{}, err
	}
	rel = filepath.ToSlash(rel)
	vs := raw[rel]
	if len(vs) == 0 {
		return time.Time{}, ErrNoVersion
	}
	pick := time.Time{}
	for _, v := range vs {
		if versionTime.IsZero() {
			if v.VersionTime.After(pick) {
				pick = v.VersionTime
			}
		} else if v.VersionTime.Equal(versionTime) {
			pick = v.VersionTime
		}
	}
	if pick.IsZero() {
		return time.Time{}, ErrNoVersion
	}
	errs, err := s.C.RestoreVersions(ctx, id, map[string]time.Time{rel: pick})
	if err != nil {
		return time.Time{}, err
	}
	if e := errs[rel]; e != "" {
		return time.Time{}, errors.New("恢复失败：" + e)
	}
	return pick, nil
}

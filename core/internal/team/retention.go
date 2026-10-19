package team

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

// Retention 是历史版本的保留规则：每个文件最多保留 Keep 份、保留 Days 天，
// 整个文件夹的历史版本总大小不超过 MaxMB（超过时先删除最旧的版本）。
type Retention struct {
	Keep  int   `json:"keep"`
	Days  int   `json:"days"`
	MaxMB int64 `json:"maxMB"`
}

var DefaultRetention = Retention{Keep: 10, Days: 30, MaxMB: 2048}

const (
	MaxKeep  = 100
	MaxDays  = 365
	MaxMaxMB = 1 << 20 // 1 TB
)

func (r Retention) Validate() error {
	switch {
	case r.Keep < 1 || r.Keep > MaxKeep:
		return fmt.Errorf("保留份数需在 1–%d 之间", MaxKeep)
	case r.Days < 1 || r.Days > MaxDays:
		return fmt.Errorf("保留天数需在 1–%d 之间", MaxDays)
	case r.MaxMB < 1 || r.MaxMB > MaxMaxMB:
		return errors.New("历史版本总容量上限需在 1 MB – 1 TB 之间")
	}
	return nil
}

// DefaultRetention 返回全局默认保留规则（未设置时为 10 份 / 30 天 / 2 GB）。
func (st *Store) DefaultRetention() Retention {
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.s.Retention != nil {
		return *st.s.Retention
	}
	return DefaultRetention
}

// FolderRetention 返回文件夹实际使用的保留规则；custom=该文件夹单独设置过。
func (st *Store) FolderRetention(id string) (Retention, bool) {
	st.mu.Lock()
	r, ok := st.s.FolderRetention[id]
	st.mu.Unlock()
	if ok {
		return r, true
	}
	return st.DefaultRetention(), false
}

// SetDefaultRetention 修改全局默认值（使用默认值的文件夹随之生效）。
func (st *Store) SetDefaultRetention(r Retention) error {
	if err := r.Validate(); err != nil {
		return err
	}
	return st.Update(func(s *Settings) { s.Retention = &r })
}

// SetFolderRetention 设置文件夹单独的保留规则；r 为 nil 表示改回使用默认值。
func (st *Store) SetFolderRetention(id string, r *Retention) error {
	if r != nil {
		if err := r.Validate(); err != nil {
			return err
		}
	}
	return st.Update(func(s *Settings) {
		if r == nil {
			delete(s.FolderRetention, id)
			return
		}
		if s.FolderRetention == nil {
			s.FolderRetention = map[string]Retention{}
		}
		s.FolderRetention[id] = *r
	})
}

// VersioningFor 返回按保留规则配置的简单版本控制。
func VersioningFor(r Retention) stclient.Versioning {
	return stclient.Versioning{Type: "simple", Params: map[string]string{
		"keep": strconv.Itoa(r.Keep), "cleanoutDays": strconv.Itoa(r.Days),
	}, CleanupIntervalS: 3600}
}

// RetentionPatch 返回需要写入引擎的版本控制配置；已一致或未开启删除保护时 ok=false。
func RetentionPatch(f stclient.Folder, r Retention) (v stclient.Versioning, ok bool) {
	if f.Versioning.Type != "simple" {
		return f.Versioning, false
	}
	keep, days := strconv.Itoa(r.Keep), strconv.Itoa(r.Days)
	if f.Versioning.Params["keep"] == keep && f.Versioning.Params["cleanoutDays"] == days {
		return f.Versioning, false
	}
	v = f.Versioning
	p := map[string]string{}
	for k, x := range f.Versioning.Params {
		p[k] = x
	}
	p["keep"], p["cleanoutDays"] = keep, days
	v.Params = p
	if v.CleanupIntervalS == 0 {
		v.CleanupIntervalS = 3600
	}
	return v, true
}

// ApplyRetention 把保留规则写入引擎（只在有变化时修改）。
func (s *Service) ApplyRetention(ctx context.Context, f stclient.Folder, r Retention) (bool, error) {
	v, ok := RetentionPatch(f, r)
	if !ok {
		return false, nil
	}
	return true, s.C.PatchFolder(ctx, f.ID, map[string]any{"versioning": v})
}

// VersionsDir 返回文件夹的历史版本目录。
func VersionsDir(f stclient.Folder) string { return versionsDir(f) }

var versionName = regexp.MustCompile(`^(.*)~(\d{8}-\d{6})([^~]*)$`)

type versionEntry struct {
	path string
	key  string // 原文件相对路径
	t    time.Time
	size int64
}

func scanVersions(dir string) ([]versionEntry, error) {
	var out []versionEntry
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == dir && errors.Is(err, fs.ErrNotExist) {
				return fs.SkipAll
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		e := versionEntry{path: p, key: rel, t: info.ModTime(), size: info.Size()}
		if m := versionName.FindStringSubmatch(d.Name()); m != nil {
			if t, err := time.ParseInLocation("20060102-150405", m[2], time.Local); err == nil {
				e.t = t
				e.key = filepath.Join(filepath.Dir(rel), m[1]+m[3])
			}
		}
		out = append(out, e)
		return nil
	})
	return out, err
}

// VersionsSize 返回历史版本目录的总大小（字节）和文件数。
func VersionsSize(dir string) (int64, int) {
	es, _ := scanVersions(dir)
	var n int64
	for _, e := range es {
		n += e.size
	}
	return n, len(es)
}

// PruneResult 是一次清理的结果。
type PruneResult struct {
	Removed int   `json:"removed"`
	Freed   int64 `json:"freed"`
	Size    int64 `json:"size"`  // 清理后的总大小
	Files   int   `json:"files"` // 清理后的版本数
}

// PruneVersions 按保留规则清理历史版本目录：先删除超过天数的版本，再删除每个文件超出份数的旧版本，
// 最后在总大小超过上限时从最旧的版本开始删除。
func PruneVersions(dir string, r Retention, now time.Time) (PruneResult, error) {
	es, err := scanVersions(dir)
	if err != nil {
		return PruneResult{}, err
	}
	var res PruneResult
	drop := map[int]bool{}
	cutoff := now.Add(-time.Duration(r.Days) * 24 * time.Hour)
	byKey := map[string][]int{}
	for i, e := range es {
		if r.Days > 0 && e.t.Before(cutoff) {
			drop[i] = true
			continue
		}
		byKey[e.key] = append(byKey[e.key], i)
	}
	if r.Keep > 0 {
		for _, idx := range byKey {
			sort.Slice(idx, func(a, b int) bool { return es[idx[a]].t.After(es[idx[b]].t) })
			for _, i := range idx[min(r.Keep, len(idx)):] {
				drop[i] = true
			}
		}
	}
	var keep []int
	var total int64
	for i := range es {
		if !drop[i] {
			keep = append(keep, i)
			total += es[i].size
		}
	}
	if limit := r.MaxMB << 20; r.MaxMB > 0 && total > limit {
		sort.Slice(keep, func(a, b int) bool { return es[keep[a]].t.Before(es[keep[b]].t) })
		for _, i := range keep {
			if total <= limit {
				break
			}
			drop[i] = true
			total -= es[i].size
		}
	}
	for i, e := range es {
		if !drop[i] {
			res.Size += e.size
			res.Files++
			continue
		}
		if err := os.Remove(e.path); err == nil || errors.Is(err, fs.ErrNotExist) {
			res.Removed++
			res.Freed += e.size
		} else {
			res.Size += e.size
			res.Files++
		}
	}
	if res.Removed > 0 {
		removeEmptyDirs(dir)
	}
	return res, nil
}

// removeEmptyDirs 删除版本目录下的空子目录（保留根目录）。
func removeEmptyDirs(root string) {
	var dirs []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.IsDir() && p != root {
			dirs = append(dirs, p)
		}
		return nil
	})
	for i := len(dirs) - 1; i >= 0; i-- {
		_ = os.Remove(dirs[i]) // 非空时失败，忽略
	}
}

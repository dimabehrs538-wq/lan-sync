package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/lan-sync/teamsync/core/internal/team"
)

func (s *Server) setMode(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Mode string `json:"mode"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	err := s.Team.SetMode(r.Context(), r.PathValue("id"), in.Mode)
	if errors.Is(err, team.ErrNotOwner) {
		writeErr(w, 403, err)
		return
	}
	respond(w, nil, err)
}

func (s *Server) setDeleteProtection(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	ret, _ := s.retentionFor(r.PathValue("id"))
	respond(w, nil, s.Team.SetDeleteProtection(r.Context(), r.PathValue("id"), in.Enabled, ret))
}

func (s *Server) revert(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, s.C.Revert(r.Context(), r.PathValue("id")))
}

func (s *Server) override(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, s.C.Override(r.Context(), r.PathValue("id")))
}

func (s *Server) listVersions(w http.ResponseWriter, r *http.Request) {
	l, err := s.Team.ListVersions(r.Context(), r.PathValue("id"))
	respond(w, l, err)
}

func (s *Server) restoreVersion(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path        string    `json:"path"`
		VersionTime time.Time `json:"versionTime"` // 省略=最近的版本
	}
	if err := decode(r, &in); err != nil || in.Path == "" {
		writeErr(w, 400, errors.New("缺少文件路径"))
		return
	}
	t, err := s.Team.RestoreVersion(r.Context(), r.PathValue("id"), in.Path, in.VersionTime)
	if errors.Is(err, team.ErrNoVersion) {
		writeErr(w, 404, err)
		return
	}
	respond(w, map[string]any{"ok": err == nil, "versionTime": t}, err)
}

// SyncModes 让本机跟随共享者设置的模式，并在动态中提示。
func (s *Server) SyncModes(ctx context.Context) {
	ch, err := s.Team.SyncModes(ctx)
	if err != nil {
		return
	}
	for _, c := range ch {
		msg := "共享者已把「" + c.Label + "」改为协作模式，你的修改会同步给其他成员"
		if c.ReadOnly {
			msg = "共享者已把「" + c.Label + "」设为只读共享，你在本机的修改不会同步给其他人"
		}
		s.Publish(UIEvent{Type: "FolderModeChanged", Time: time.Now(), Notify: true, Message: msg, Data: map[string]any{"folderID": c.FolderID, "readOnly": c.ReadOnly}})
	}
}

var modeSyncPending atomic.Bool

// syncModesSoon 在模式文件到达后尽快同步（文件可能还在下载，稍后再检查一次）。
func (s *Server) syncModesSoon(ctx context.Context) {
	if !modeSyncPending.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer modeSyncPending.Store(false)
		for _, d := range []time.Duration{time.Second, 5 * time.Second} {
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
			s.SyncModes(ctx)
		}
	}()
}

// GuardInterval 后台检查（位置重叠、共享模式）的间隔；TEAMSYNC_GUARD_INTERVAL_S 可覆盖（测试用）。
func GuardInterval() time.Duration {
	if v, err := strconv.Atoi(os.Getenv("TEAMSYNC_GUARD_INTERVAL_S")); err == nil && v > 0 {
		return time.Duration(v) * time.Second
	}
	return time.Minute
}

// activityData 提取需要随动态保存的信息（删除记录：文件夹 ID、路径，用于一键恢复）。
func activityData(e UIEvent) map[string]string {
	if e.Type != "RemoteChangeDetected" {
		return nil
	}
	m, _ := e.Data.(map[string]any)
	out := map[string]string{}
	for _, k := range []string{"folderID", "path", "action", "type", "modifiedBy"} {
		if v, ok := m[k].(string); ok {
			out[k] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

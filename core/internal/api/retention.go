package api

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
)

// pruneState 记录历史版本清理的状态：有变化的文件夹、上次清理时间、大小缓存。
type pruneState struct {
	mu    sync.Mutex
	dirty map[string]bool
	last  map[string]time.Time
	size  map[string]sizeInfo
}

type sizeInfo struct {
	bytes int64
	files int
	at    time.Time
}

func (p *pruneState) init() {
	if p.dirty == nil {
		p.dirty, p.last, p.size = map[string]bool{}, map[string]time.Time{}, map[string]sizeInfo{}
	}
}

// markVersionsDirty 文件夹收到远程修改后调用：下一轮后台检查时清理它的历史版本。
func (s *Server) markVersionsDirty(id string) {
	s.prune.mu.Lock()
	defer s.prune.mu.Unlock()
	s.prune.init()
	s.prune.dirty[id] = true
}

func (s *Server) retentionFor(id string) (team.Retention, bool) {
	if s.Store == nil {
		return team.DefaultRetention, false
	}
	return s.Store.FolderRetention(id)
}

// fullPruneEvery 无变化时也定期完整清理一次的间隔（按天数过期的版本需要定期删除）。
func fullPruneEvery() time.Duration {
	if d := 10 * GuardInterval(); d < 10*time.Minute {
		return d
	}
	return 10 * time.Minute
}

// pruneFolder 按保留规则清理一个文件夹的历史版本并更新大小缓存。
func (s *Server) pruneFolder(f stclient.Folder) (team.PruneResult, error) {
	r, _ := s.retentionFor(f.ID)
	res, err := team.PruneVersions(team.VersionsDir(f), r, time.Now())
	s.prune.mu.Lock()
	s.prune.init()
	delete(s.prune.dirty, f.ID)
	s.prune.last[f.ID] = time.Now()
	if err == nil {
		s.prune.size[f.ID] = sizeInfo{res.Size, res.Files, time.Now()}
	}
	s.prune.mu.Unlock()
	return res, err
}

// GuardRetention 后台检查：把保留规则同步到引擎，并清理有变化或到期的文件夹。
// force=true 时清理所有开启删除保护的文件夹（例如修改了默认值）。
func (s *Server) GuardRetention(ctx context.Context, force bool) {
	fs, err := s.C.Folders(ctx)
	if err != nil {
		return
	}
	for _, f := range fs {
		if f.Versioning.Type == "" {
			continue
		}
		r, _ := s.retentionFor(f.ID)
		_, _ = s.Team.ApplyRetention(ctx, f, r)
		s.prune.mu.Lock()
		s.prune.init()
		due := force || s.prune.dirty[f.ID] || time.Since(s.prune.last[f.ID]) >= fullPruneEvery()
		s.prune.mu.Unlock()
		if due {
			_, _ = s.pruneFolder(f)
		}
	}
}

// versionsSize 返回历史版本占用（20 秒缓存）。
func (s *Server) versionsSize(f stclient.Folder) (int64, int) {
	s.prune.mu.Lock()
	s.prune.init()
	c, ok := s.prune.size[f.ID]
	s.prune.mu.Unlock()
	if ok && time.Since(c.at) < 20*time.Second {
		return c.bytes, c.files
	}
	b, n := team.VersionsSize(team.VersionsDir(f))
	s.prune.mu.Lock()
	s.prune.size[f.ID] = sizeInfo{b, n, time.Now()}
	s.prune.mu.Unlock()
	return b, n
}

type retentionView struct {
	team.Retention
	Custom bool `json:"custom"`
}

func (s *Server) setFolderRetention(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Custom bool `json:"custom"`
		team.Retention
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if s.Store == nil {
		writeErr(w, 500, errNoStore)
		return
	}
	id := r.PathValue("id")
	f, err := s.C.Folder(r.Context(), id)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	var rp *team.Retention
	if in.Custom {
		rp = &in.Retention
	}
	if err := s.Store.SetFolderRetention(id, rp); err != nil {
		writeErr(w, 400, err)
		return
	}
	ret, _ := s.retentionFor(id)
	if _, err := s.Team.ApplyRetention(r.Context(), f, ret); err != nil {
		writeErr(w, 500, err)
		return
	}
	res, _ := s.pruneFolder(f)
	writeJSON(w, map[string]any{"retention": retentionView{ret, in.Custom}, "cleanup": res})
}

func (s *Server) cleanupVersions(w http.ResponseWriter, r *http.Request) {
	f, err := s.C.Folder(r.Context(), r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	res, err := s.pruneFolder(f)
	respond(w, res, err)
}

var errNoStore = errors.New("设置存储不可用")

package api

import (
	"context"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
)

var isWin = runtime.GOOS == "windows"

// resolve 与 team 包的位置检查使用同一套解析（绝对路径 + 符号链接）。
var resolve = team.ResolvePath

// addedAt 返回文件夹添加时间：优先用本机记录，否则用 .stfolder 的修改时间（并记录下来保持稳定）。
func (s *Server) addedAt(f stclient.Folder) time.Time {
	if s.Store != nil {
		if t := s.Store.Get().AddedAt[f.ID]; !t.IsZero() {
			return t
		}
		if t := team.MarkerTime(f); !t.IsZero() {
			s.Store.RecordAdded(f.ID, t)
			return t
		}
	}
	return time.Time{}
}

func (s *Server) overlaps(fs []stclient.Folder) map[string]team.Overlap {
	return team.FindOverlaps(fs, isWin, resolve, s.addedAt)
}

func (s *Server) checkPath(ctx context.Context, p, excludeID string) error {
	fs, err := s.C.Folders(ctx)
	if err != nil {
		return err
	}
	base := ""
	if s.Store != nil {
		base = s.Store.Get().DefaultBaseDir
	}
	return s.Team.CheckPath(fs, p, base, excludeID)
}

// pathCheck：界面在提交前检查位置是否可用、目录是否非空。
func (s *Server) pathCheck(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path     string `json:"path"`
		FolderID string `json:"folderID"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	info := team.InspectPath(in.Path)
	if err := s.checkPath(r.Context(), in.Path, in.FolderID); err != nil {
		info.OK, info.Error = false, err.Error()
	}
	writeJSON(w, info)
}

// GuardOverlaps 检查位置相同或互相嵌套的文件夹：暂停后添加的一方并提示；
// 之前因重叠被暂停、现在已不再重叠的（例如另一方已移除）自动恢复。
func (s *Server) GuardOverlaps(ctx context.Context) {
	fs, err := s.C.Folders(ctx)
	if err != nil {
		return
	}
	ov := s.overlaps(fs)
	marked := s.Store.Get().OverlapPaused
	for _, f := range fs {
		o, bad := ov[f.ID]
		switch {
		case bad && !f.Paused:
			if err := s.C.PatchFolder(ctx, f.ID, map[string]any{"paused": true}); err != nil {
				continue
			}
			_ = s.Store.Update(func(st *team.Settings) {
				if st.OverlapPaused == nil {
					st.OverlapPaused = map[string]bool{}
				}
				st.OverlapPaused[f.ID] = true
			})
			name := f.Label
			if name == "" {
				name = f.ID
			}
			s.Publish(UIEvent{Type: "FolderOverlap", Time: time.Now(), Notify: true,
				Message: "文件夹「" + name + "」与「" + o.WithLabel + "」位置重叠，已暂停同步，请重新选择位置",
				Data:    map[string]any{"folderID": f.ID, "with": o.With}})
		case !bad && marked[f.ID]:
			if _, err := os.Stat(f.Path); err == nil {
				_ = s.C.PatchFolder(ctx, f.ID, map[string]any{"paused": false})
			}
			_ = s.Store.Update(func(st *team.Settings) { delete(st.OverlapPaused, f.ID) })
		}
	}
}

// RunOverlapGuard 启动时及之后每分钟检查一次。
func (s *Server) RunOverlapGuard(ctx context.Context, every time.Duration) {
	s.GuardOverlaps(ctx)
	s.SyncModes(ctx)
	s.GuardRetention(ctx, false)
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.GuardOverlaps(ctx)
			s.SyncModes(ctx)
			s.GuardRetention(ctx, false)
		}
	}
}

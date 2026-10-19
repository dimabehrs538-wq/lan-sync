package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"

	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
)

// ownerOf 返回本机记录的共享者（未记录或未知时为空）。
func (s *Server) ownerOf(id string) string {
	if s.Store == nil {
		return ""
	}
	o, _ := s.Store.Owner(id)
	return o
}

var (
	ignoredMu   sync.Mutex
	ignoredLast = map[string]string{}
)

// logIgnoredMode 记录被忽略的模式文件（同一文件夹同一原因只记一次）。
func logIgnoredMode(f stclient.Folder, reason string) {
	ignoredMu.Lock()
	defer ignoredMu.Unlock()
	if ignoredLast[f.ID] == reason {
		return
	}
	ignoredLast[f.ID] = reason
	log.Printf("忽略文件夹 %s 的 %s：%s", f.ID, team.ModeFile, reason)
}

// MigrateOwners 为旧版本升级上来的文件夹记录共享者（只处理还没有记录的文件夹）。
func (s *Server) MigrateOwners(ctx context.Context) {
	if s.Store == nil {
		return
	}
	for _, l := range s.Team.MigrateOwners(ctx, s.Store) {
		log.Print(l)
	}
}

// deviceName 返回设备名称（本机为“我”）。
func (s *Server) deviceName(ctx context.Context, id string) string {
	if my, _ := s.Team.MyID(ctx); id == my {
		return "我"
	}
	if devs, err := s.C.Devices(ctx); err == nil {
		for _, d := range devs {
			if d.DeviceID == id && d.Name != "" {
				return d.Name
			}
		}
	}
	return stclient.ShortID(id)
}

// modeError 把共享模式的权限错误转为带共享者名称的中文提示。
func (s *Server) modeError(ctx context.Context, w http.ResponseWriter, id string, err error) bool {
	switch {
	case errors.Is(err, team.ErrNotOwner):
		writeErr(w, 403, fmt.Errorf("只有共享者（%s）可以修改这个文件夹的共享模式", s.deviceName(ctx, s.ownerOf(id))))
	case errors.Is(err, team.ErrOwnerUnknown):
		writeErr(w, 409, err)
	default:
		return false
	}
	return true
}

// setOwner 共享者未知时由用户在本机指定（必须是文件夹中的设备）；已确定的共享者不能修改。
func (s *Server) setOwner(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID string `json:"deviceID"`
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
	found := false
	for _, d := range f.Devices {
		found = found || d.DeviceID == in.DeviceID
	}
	if !found {
		writeErr(w, 400, errors.New("共享者必须是这个文件夹中的设备"))
		return
	}
	if err := s.Store.AssignOwner(id, in.DeviceID); err != nil {
		writeErr(w, 409, err)
		return
	}
	respond(w, nil, nil)
}

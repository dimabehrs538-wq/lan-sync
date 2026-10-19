// Package api 为 Tauri 前端提供本地 HTTP API（仅监听 127.0.0.1，Bearer token 鉴权，SSE 推送事件）。
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/lan-sync/teamsync/core/internal/activity"
	"github.com/lan-sync/teamsync/core/internal/conflict"
	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
	"github.com/lan-sync/teamsync/core/internal/trash"
)

// UIEvent 是推给前端的简化事件，Message 为中文提示。
type UIEvent struct {
	Type    string    `json:"type"`
	Message string    `json:"message,omitempty"`
	Notify  bool      `json:"notify"`
	Time    time.Time `json:"time"`
	Data    any       `json:"data,omitempty"`
	// Key 非空时，最近动态中同 Key 的记录原地更新（例如反复断线合并为一条）
	Key        string `json:"key,omitempty"`
	ActivityID int64  `json:"activityId,omitempty"`
	merge      time.Duration
}

// presenceTypes 为上下线类事件，是否弹系统通知由设置“上下线通知”决定（默认关闭）
var presenceTypes = map[string]bool{"DeviceConnected": true, "DeviceDisconnected": true, "DeviceUnstable": true}

type Server struct {
	C           *stclient.Client
	Team        *team.Service
	Token       string
	EngineState func() string
	Store       *team.Store
	Activity    *activity.Log
	Presence    *Presence

	mu   sync.Mutex
	subs map[chan UIEvent]struct{}
}

func New(c *stclient.Client, token string, state func() string, store *team.Store) *Server {
	s := &Server{C: c, Team: &team.Service{C: c}, Token: token, EngineState: state, Store: store, Activity: activity.Open(""), subs: map[chan UIEvent]struct{}{}}
	s.Presence = NewPresence(s.Publish)
	return s
}

func (s *Server) Publish(e UIEvent) {
	if presenceTypes[e.Type] {
		e.Notify = e.Message != "" && s.Store != nil && s.Store.Get().PresenceNotify
	}
	if e.Message != "" && s.Activity != nil {
		it := s.Activity.Add(activity.Item{Type: e.Type, Message: e.Message, Time: e.Time, Key: e.Key, Data: activityData(e)}, e.merge)
		e.ActivityID = it.ID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.subs {
		select {
		case ch <- e:
		default:
		}
	}
}

func (s *Server) Handler() http.Handler {
	m := http.NewServeMux()
	m.HandleFunc("GET /api/status", s.status)
	m.HandleFunc("GET /api/folders", s.folders)
	m.HandleFunc("POST /api/folders", s.createFolder)
	m.HandleFunc("POST /api/folders/{id}/scan", s.scan)
	m.HandleFunc("POST /api/folders/{id}/pause", s.pauseFolder(true))
	m.HandleFunc("POST /api/folders/{id}/resume", s.pauseFolder(false))
	m.HandleFunc("GET /api/devices", s.devices)
	m.HandleFunc("POST /api/devices", s.addDevice)
	m.HandleFunc("DELETE /api/devices/{id}", s.delDevice)
	m.HandleFunc("PATCH /api/devices/{id}", s.renameDevice)
	m.HandleFunc("GET /api/me", s.getMe)
	m.HandleFunc("PUT /api/me", s.putMe)
	m.HandleFunc("GET /api/nearby", s.nearby)
	m.HandleFunc("POST /api/pending/devices/{id}/accept", s.acceptDevice)
	m.HandleFunc("DELETE /api/pending/devices/{id}", s.dismissDevice)
	m.HandleFunc("PUT /api/folders/{id}/members", s.setMembers)
	m.HandleFunc("POST /api/folders/{id}/repair", s.repairFolder)
	m.HandleFunc("POST /api/folders/{id}/relocate", s.relocateFolder)
	m.HandleFunc("DELETE /api/folders/{id}", s.removeFolder)
	m.HandleFunc("POST /api/folders/{id}/reinvite", s.reinvite)
	m.HandleFunc("GET /api/invitations", s.invitations)
	m.HandleFunc("POST /api/invitations/accept", s.acceptInvitation)
	m.HandleFunc("POST /api/invitations/ignore", s.ignoreInvitation)
	m.HandleFunc("GET /api/settings", s.getSettings)
	m.HandleFunc("PUT /api/settings", s.putSettings)
	m.HandleFunc("GET /api/conflicts", s.conflicts)
	m.HandleFunc("POST /api/conflicts/resolve", s.resolveConflict)
	m.HandleFunc("GET /api/events", s.events)
	m.HandleFunc("GET /api/activity", s.listActivity)
	m.HandleFunc("POST /api/path-check", s.pathCheck)
	m.HandleFunc("PUT /api/folders/{id}/mode", s.setMode)
	m.HandleFunc("PUT /api/folders/{id}/delete-protection", s.setDeleteProtection)
	m.HandleFunc("POST /api/folders/{id}/revert", s.revert)
	m.HandleFunc("POST /api/folders/{id}/override", s.override)
	m.HandleFunc("GET /api/folders/{id}/versions", s.listVersions)
	m.HandleFunc("POST /api/folders/{id}/versions/restore", s.restoreVersion)
	m.HandleFunc("DELETE /api/activity", s.clearActivity)
	return s.cors(s.auth(m))
}

func (s *Server) cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(204)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func (s *Server) auth(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if tok == "" {
			tok = r.URL.Query().Get("token") // EventSource 无法设置请求头
		}
		if subtle.ConstantTimeCompare([]byte(tok), []byte(s.Token)) != 1 {
			writeErr(w, 401, fmt.Errorf("未授权"))
			return
		}
		h.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": FriendlyError(err)})
}

// FriendlyError 把底层错误转换为中文提示。
func FriendlyError(err error) string {
	var ae *stclient.APIError
	if errors.As(err, &ae) {
		body := strings.TrimSpace(ae.Body)
		switch {
		case ae.Status == 404:
			return "找不到对应的文件夹或设备，可能已被移除"
		case ae.Status == 403 || ae.Status == 401:
			return "同步引擎拒绝访问，请重启软件"
		case strings.Contains(strings.ToLower(body), "marker missing"), strings.Contains(strings.ToLower(body), "folder path missing"):
			return team.TranslateEngineError(body)
		case strings.Contains(body, "device ID"):
			return "设备 ID 格式不正确，请检查后重试"
		case strings.Contains(body, "path"):
			return "路径无效或无权限访问：" + body
		}
		return fmt.Sprintf("同步引擎返回错误（%d）：%s", ae.Status, body)
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "actively refused"), strings.Contains(msg, "EOF"), strings.Contains(msg, "connection reset"):
		return "同步引擎暂时不可用（可能正在重启），请稍后重试"
	case strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "Timeout"):
		return "同步引擎响应超时，请稍后重试"
	case strings.Contains(msg, "permission denied"), strings.Contains(msg, "Access is denied"):
		return "没有权限访问该路径"
	case strings.Contains(msg, "invalid character"), strings.Contains(msg, "cannot unmarshal"):
		return "请求格式不正确"
	}
	return msg
}

func respond(w http.ResponseWriter, v any, err error) {
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	if v == nil {
		v = map[string]bool{"ok": true}
	}
	writeJSON(w, v)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := map[string]any{"engine": s.EngineState()}
	st, err := s.C.Status(ctx)
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, out)
		return
	}
	out["myID"] = st.MyID
	out["uptime"] = st.Uptime
	if v, err := s.C.Version(ctx); err == nil {
		out["syncthingVersion"] = v.Version
	}
	conns, _ := s.C.Connections(ctx)
	n := 0
	for id, c := range conns.Connections {
		if c.Connected && id != st.MyID {
			n++
		}
	}
	out["connected"] = n
	folders, _ := s.C.Folders(ctx)
	syncing := false
	for _, f := range folders {
		if fs, err := s.C.FolderStatus(ctx, f.ID); err == nil && (fs.State == "syncing" || fs.State == "scanning" || fs.NeedFiles > 0) {
			syncing = true
		}
	}
	out["folders"] = len(folders)
	out["syncing"] = syncing
	writeJSON(w, out)
}

type folderMember struct {
	DeviceID    string  `json:"deviceID"`
	Name        string  `json:"name"`
	Connected   bool    `json:"connected"`
	RemoteState string  `json:"remoteState"` // valid=已接受 notSharing=未接受/已忽略 paused unknown
	Completion  float64 `json:"completion"`
}

type folderView struct {
	stclient.Folder
	Status    *stclient.FolderStatus `json:"status,omitempty"`
	Conflicts int                    `json:"conflicts"`
	Members   []folderMember         `json:"members"`
	Problem   *team.Problem          `json:"problem,omitempty"`
	// Mode 共享模式 collab|readonly；Role 只读时本机角色 owner(共享者)|member(成员)
	Mode             string `json:"mode"`
	Role             string `json:"role,omitempty"`
	DeleteProtection bool   `json:"deleteProtection"`
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request) {
	fs, err := s.C.Folders(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	ctx := r.Context()
	my, _ := s.Team.MyID(ctx)
	devs, _ := s.C.Devices(ctx)
	names := map[string]string{}
	for _, d := range devs {
		names[d.DeviceID] = d.Name
	}
	conns, _ := s.C.Connections(ctx)
	overlaps := s.overlaps(fs)
	out := make([]folderView, 0, len(fs))
	for _, f := range fs {
		v := folderView{Folder: f, Members: []folderMember{}, DeleteProtection: f.Versioning.Type != ""}
		v.Mode, v.Role = team.FolderMode(f, my)
		v.Folder.Label, _ = team.ParseLabel(f.Label)
		for _, d := range f.Devices {
			if d.DeviceID == my {
				continue
			}
			m := folderMember{DeviceID: d.DeviceID, Name: names[d.DeviceID], Connected: conns.Connections[d.DeviceID].Connected, RemoteState: "unknown"}
			if c, err := s.C.Completion(ctx, f.ID, d.DeviceID); err == nil {
				m.RemoteState, m.Completion = c.RemoteState, c.Completion
			}
			v.Members = append(v.Members, m)
		}
		stErr := ""
		if st, err := s.C.FolderStatus(r.Context(), f.ID); err == nil {
			v.Status = &st
			stErr = st.Error
		}
		if o, bad := overlaps[f.ID]; bad {
			v.Problem = &team.Problem{Kind: "overlap", Message: team.OverlapMessage(o), PathExists: true}
		} else {
			v.Problem = team.Diagnose(f, stErr)
		}
		if cs, err := conflict.Scan(f.Path); err == nil {
			v.Conflicts = len(cs)
		}
		out = append(out, v)
	}
	writeJSON(w, out)
}

func decode(r *http.Request, v any) error { return json.NewDecoder(r.Body).Decode(v) }

func (s *Server) createFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label            string    `json:"label"`
		Path             string    `json:"path"`
		Members          *[]string `json:"members"`          // 省略=全部成员
		Mode             string    `json:"mode"`             // collab(默认) | readonly
		DeleteProtection *bool     `json:"deleteProtection"` // 省略=开启
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	var members []string
	if in.Members != nil {
		members = append([]string{}, (*in.Members)...)
	}
	if err := s.checkPath(r.Context(), in.Path, ""); err != nil {
		writeErr(w, 400, err)
		return
	}
	in.Label, _ = team.ParseLabel(strings.TrimSpace(in.Label))
	f, err := s.Team.CreateFolder(r.Context(), in.Label, in.Path, members)
	if err == nil && s.Store != nil {
		s.Store.RecordAdded(f.ID, time.Now())
	}
	if err == nil && in.Mode == team.ModeReadOnly {
		err = s.Team.SetMode(r.Context(), f.ID, team.ModeReadOnly)
		f.Type = "sendonly"
	}
	if err == nil && in.DeleteProtection != nil && !*in.DeleteProtection {
		err = s.Team.SetDeleteProtection(r.Context(), f.ID, false)
		f.Versioning = stclient.Versioning{}
	}
	respond(w, f, err)
}

func (s *Server) repairFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Force bool `json:"force"`
	}
	_ = decode(r, &in)
	err := s.Team.Repair(r.Context(), r.PathValue("id"), in.Force)
	switch {
	case errors.Is(err, team.ErrNeedConfirm):
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(409)
		_ = json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "needConfirm": true})
	case errors.Is(err, team.ErrPathMissing):
		writeErr(w, 409, err)
	default:
		respond(w, nil, err)
	}
}

func (s *Server) relocateFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Path string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	id := r.PathValue("id")
	if err := s.checkPath(r.Context(), in.Path, id); err != nil {
		writeErr(w, 400, err)
		return
	}
	if err := s.Team.Relocate(r.Context(), id, in.Path); err != nil {
		writeErr(w, 500, err)
		return
	}
	// 因位置重叠被自动暂停的文件夹，换到安全位置后恢复同步
	if s.Store.Get().OverlapPaused[id] {
		_ = s.C.PatchFolder(r.Context(), id, map[string]any{"paused": false})
		_ = s.Store.Update(func(st *team.Settings) { delete(st.OverlapPaused, id) })
	}
	respond(w, nil, nil)
}

func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, s.C.Scan(r.Context(), r.PathValue("id"), ""))
}

func (s *Server) pauseFolder(p bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !p {
			if fs, err := s.C.Folders(r.Context()); err == nil {
				if o, bad := s.overlaps(fs)[id]; bad {
					writeErr(w, 409, errors.New(team.OverlapMessage(o)))
					return
				}
			}
		}
		respond(w, nil, s.C.PatchFolder(r.Context(), id, map[string]any{"paused": p}))
	}
}

type deviceView struct {
	stclient.Device
	Connected bool   `json:"connected"`
	Address   string `json:"address,omitempty"`
	Self      bool   `json:"self"`
	Remark    bool   `json:"remark"`                  // 本机手动设置过备注
	Announced string `json:"announcedName,omitempty"` // 对方自己设置的名称
}

func (s *Server) devices(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	devs, err := s.C.Devices(ctx)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	my, _ := s.Team.MyID(ctx)
	conns, _ := s.C.Connections(ctx)
	out := make([]deviceView, 0, len(devs))
	for _, d := range devs {
		c := conns.Connections[d.DeviceID]
		out = append(out, deviceView{Device: d, Connected: c.Connected, Address: c.Address, Self: d.DeviceID == my,
			Remark: s.Store.HasRemark(d.DeviceID), Announced: s.Store.Announced(d.DeviceID)})
	}
	writeJSON(w, out)
}

func (s *Server) addDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID string `json:"deviceID"`
		Name     string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	err := s.Team.AddMember(r.Context(), in.DeviceID, in.Name)
	if err == nil && strings.TrimSpace(in.Name) != "" {
		_ = s.Store.SetRemark(strings.TrimSpace(in.DeviceID), true)
	}
	respond(w, nil, err)
}

func (s *Server) delDevice(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, s.C.DeleteDevice(r.Context(), r.PathValue("id")))
}

func (s *Server) renameDevice(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name         string `json:"name"`
		UseAnnounced bool   `json:"useAnnounced"` // 清除备注，改用对方自己设置的名称
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	id := r.PathValue("id")
	if in.UseAnnounced {
		name, err := s.Team.ClearRemark(r.Context(), s.Store, id)
		respond(w, map[string]string{"name": name}, err)
		return
	}
	name, err := s.Team.RenameDevice(r.Context(), id, in.Name)
	if err == nil {
		err = s.Store.SetRemark(id, true)
	}
	switch {
	case errors.Is(err, team.ErrDeviceNotFound):
		writeErr(w, 404, err)
	case err != nil && !isEngineErr(err):
		writeErr(w, 400, err)
	default:
		respond(w, map[string]string{"name": name}, err)
	}
}

func (s *Server) getMe(w http.ResponseWriter, r *http.Request) {
	id, name, err := s.Team.MyName(r.Context())
	respond(w, map[string]string{"deviceID": id, "name": name}, err)
}

func (s *Server) putMe(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if _, err := team.NormalizeName(in.Name); err != nil {
		writeErr(w, 400, err)
		return
	}
	name, err := s.Team.SetMyName(r.Context(), in.Name)
	respond(w, map[string]string{"name": name}, err)
}

func isEngineErr(err error) bool {
	var ae *stclient.APIError
	return errors.As(err, &ae) || strings.Contains(err.Error(), "dial") || strings.Contains(err.Error(), "EOF")
}

func (s *Server) nearby(w http.ResponseWriter, r *http.Request) {
	n, err := s.Team.NearbyDevices(r.Context())
	respond(w, n, err)
}

func (s *Server) acceptDevice(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, s.Team.AcceptPending(r.Context(), r.PathValue("id")))
}

func (s *Server) dismissDevice(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, s.C.DismissPendingDevice(r.Context(), r.PathValue("id")))
}

// removeFolder 仅在本机移除文件夹。?deleteFiles=1 同时删除本地文件（优先回收站），
// &permanent=1 允许回收站失败时永久删除。
func (s *Server) removeFolder(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	res, err := s.Team.RemoveFolder(r.Context(), s.Store, r.PathValue("id"), q.Get("deleteFiles") == "1", q.Get("permanent") == "1", trash.Remove)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	writeJSON(w, res)
}

func (s *Server) setMembers(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Members []string `json:"members"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	respond(w, nil, s.Team.SetFolderMembers(r.Context(), r.PathValue("id"), in.Members))
}

// ReinviteGap 重新邀请时移除与加回之间的间隔（需大于集群配置传播时间）。
var ReinviteGap = 5 * time.Second

func (s *Server) reinvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DeviceID string `json:"deviceID"`
	}
	if err := decode(r, &in); err != nil || in.DeviceID == "" {
		writeErr(w, 400, fmt.Errorf("缺少 deviceID"))
		return
	}
	fid := r.PathValue("id")
	if _, err := s.C.Folder(r.Context(), fid); err != nil {
		writeErr(w, 404, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_ = s.Team.Reinvite(ctx, fid, in.DeviceID, ReinviteGap)
	}()
	respond(w, nil, nil)
}

func (s *Server) invitations(w http.ResponseWriter, r *http.Request) {
	inv, err := s.Team.Invitations(r.Context(), s.Store)
	respond(w, inv, err)
}

func (s *Server) acceptInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FolderID string `json:"folderID"`
		DeviceID string `json:"deviceID"`
		Label    string `json:"label"`
		Path     string `json:"path"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	if in.FolderID == "" || in.Path == "" {
		writeErr(w, 400, fmt.Errorf("文件夹和保存路径不能为空"))
		return
	}
	ctx := r.Context()
	if err := s.checkPath(ctx, in.Path, in.FolderID); err != nil {
		writeErr(w, 400, err)
		return
	}
	my, err := s.Team.MyID(ctx)
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	// 同一文件夹可能由多位成员共享，一起加入
	devs := []string{my, in.DeviceID}
	if pend, err := s.C.PendingFolders(ctx); err == nil {
		for d := range pend[in.FolderID].OfferedBy {
			devs = append(devs, d)
		}
	}
	if err := os.MkdirAll(in.Path, 0o755); err != nil {
		writeErr(w, 400, err)
		return
	}
	label, ro := team.ParseLabel(in.Label)
	if pend, err := s.C.PendingFolders(ctx); err == nil {
		if o, ok := pend[in.FolderID].OfferedBy[in.DeviceID]; ok {
			_, ro = team.ParseLabel(o.Label) // 以邀请携带的模式为准
		}
	}
	f := team.NewTeamFolder(label, in.Path, devs)
	f.ID = in.FolderID
	if ro {
		f.Type = "receiveonly" // 只读共享：本机改动不会同步出去
	}
	if err := s.C.PutFolder(ctx, f); err != nil {
		writeErr(w, 500, err)
		return
	}
	_ = s.Store.Unignore(in.FolderID)
	// 重新接受（例如移除后再加入）算作新添加
	_ = s.Store.Update(func(st *team.Settings) { delete(st.AddedAt, in.FolderID) })
	s.Store.RecordAdded(in.FolderID, time.Now())
	respond(w, f, nil)
}

func (s *Server) ignoreInvitation(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FolderID  string `json:"folderID"`
		DeviceID  string `json:"deviceID"`
		OfferTime string `json:"offerTime"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	respond(w, nil, s.Store.Ignore(in.FolderID, in.DeviceID, in.OfferTime))
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	st := s.Store.Get()
	writeJSON(w, map[string]any{"defaultBaseDir": st.DefaultBaseDir, "ignoredCount": len(st.Ignored), "presenceNotify": st.PresenceNotify})
}

func (s *Server) listActivity(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Activity.List())
}
func (s *Server) clearActivity(w http.ResponseWriter, r *http.Request) {
	s.Activity.Clear()
	respond(w, nil, nil)
}

func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var in struct {
		DefaultBaseDir string `json:"defaultBaseDir"`
		PresenceNotify *bool  `json:"presenceNotify"`
	}
	if err := decode(r, &in); err != nil || (in.DefaultBaseDir == "" && in.PresenceNotify == nil) {
		writeErr(w, 400, fmt.Errorf("默认目录不能为空"))
		return
	}
	if in.PresenceNotify != nil {
		if err := s.Store.Update(func(st *team.Settings) { st.PresenceNotify = *in.PresenceNotify }); err != nil {
			writeErr(w, 500, err)
			return
		}
		if in.DefaultBaseDir == "" {
			respond(w, nil, nil)
			return
		}
	}
	if err := os.MkdirAll(in.DefaultBaseDir, 0o755); err != nil {
		writeErr(w, 400, fmt.Errorf("无法创建目录：%v", err))
		return
	}
	if err := s.Store.Update(func(st *team.Settings) { st.DefaultBaseDir = in.DefaultBaseDir }); err != nil {
		writeErr(w, 500, err)
		return
	}
	respond(w, nil, s.Team.ApplyFolderDefaults(r.Context(), in.DefaultBaseDir))
}

type conflictView struct {
	conflict.Conflict
	FolderID    string `json:"folderID"`
	FolderLabel string `json:"folderLabel"`
}

func (s *Server) conflicts(w http.ResponseWriter, r *http.Request) {
	fs, err := s.C.Folders(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	out := []conflictView{}
	for _, f := range fs {
		cs, _ := conflict.Scan(f.Path)
		for _, c := range cs {
			out = append(out, conflictView{Conflict: c, FolderID: f.ID, FolderLabel: f.Label})
		}
	}
	writeJSON(w, out)
}

func (s *Server) resolveConflict(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FolderID string `json:"folderID"`
		Path     string `json:"path"`
		Keep     string `json:"keep"` // "conflict" | "original"
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	f, err := s.C.Folder(r.Context(), in.FolderID)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	respond(w, nil, conflict.Resolve(f.Path, in.Path, in.Keep == "conflict"))
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, fmt.Errorf("不支持流"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch := make(chan UIEvent, 64)
	s.mu.Lock()
	s.subs[ch] = struct{}{}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.subs, ch); s.mu.Unlock() }()
	fmt.Fprint(w, ": ok\n\n")
	fl.Flush()
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
			fl.Flush()
		case e := <-ch:
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		}
	}
}

// RunEventBridge 订阅 Syncthing 事件，翻译成中文 UIEvent 并广播。
func (s *Server) RunEventBridge(ctx context.Context) {
	names := newNameCache(s.C)
	seenPending := map[string]bool{}
	seenInvite := map[string]bool{}
	s.C.WatchEvents(ctx, func(e stclient.Event) {
		if e.Type == "ConfigSaved" {
			names.Invalidate()
		}
		if e.Type == "DeviceConnected" {
			var d struct {
				ID         string `json:"id"`
				DeviceName string `json:"deviceName"`
			}
			if json.Unmarshal(e.Data, &d) == nil {
				if changed, _ := s.Team.ApplyAnnounced(ctx, s.Store, d.ID, d.DeviceName); changed {
					names.Invalidate()
				}
			}
		}
		if e.Type == "ItemFinished" || e.Type == "RemoteChangeDetected" {
			var d struct {
				Item string `json:"item"`
				Path string `json:"path"`
			}
			if json.Unmarshal(e.Data, &d) == nil && (d.Item == team.ModeFile || d.Path == team.ModeFile) {
				s.syncModesSoon(ctx)
			}
			if e.Type == "ItemFinished" {
				return
			}
		}
		if e.Type == "DeviceConnected" || e.Type == "DeviceDisconnected" {
			var d struct {
				ID string `json:"id"`
			}
			if json.Unmarshal(e.Data, &d) == nil && d.ID != "" {
				name := names.Lookup(ctx)(d.ID)
				if e.Type == "DeviceConnected" {
					s.Presence.Connected(d.ID, name)
				} else {
					s.Presence.Disconnected(d.ID, name)
				}
			}
			return
		}
		if ue, ok := Translate(e, names.Lookup(ctx)); ok {
			if e.Type == "PendingFoldersChanged" {
				// 对方撤回邀请（移出成员/重新邀请的第一步）-> 清除忽略记录，下次邀请重新出现
				if m, _ := ue.Data.(map[string]any); m != nil {
					rem, _ := m["removed"].([]any)
					for _, x := range rem {
						if o, _ := x.(map[string]any); o != nil {
							fid, _ := o["folderID"].(string)
							dev, _ := o["deviceID"].(string)
							s.Store.UnignorePair(fid, dev)
						}
					}
				}
				inv, err := s.Team.Invitations(ctx, s.Store)
				if err != nil {
					return
				}
				for _, i := range inv {
					k := i.FolderID + "|" + i.DeviceID + "|" + i.OfferTime
					if seenInvite[k] {
						continue
					}
					seenInvite[k] = true
					who := i.DeviceName
					if who == "" {
						who = i.DeviceID[:7]
					}
					s.Publish(UIEvent{Type: "FolderInvitation", Time: e.Time, Notify: true,
						Message: who + " 邀请你加入文件夹「" + i.Label + "」", Data: i})
				}
				s.Publish(UIEvent{Type: "InvitationsChanged", Time: e.Time})
				return
			}
			// Syncthing 会周期性重复报告待确认设备，同一设备只提示一次
			if e.Type == "PendingDevicesChanged" {
				if m, _ := ue.Data.(map[string]any); m != nil {
					id, _ := m["deviceID"].(string)
					if seenPending[id] {
						return
					}
					seenPending[id] = true
				}
			}
			s.Publish(ue)
		}
	})
}

type nameCache struct {
	c  *stclient.Client
	at time.Time
	m  map[string]string
	mu sync.Mutex
}

func newNameCache(c *stclient.Client) *nameCache { return &nameCache{c: c} }

// Lookup 返回 设备ID(或7位短ID) -> 名称 的查询函数。
// Invalidate 让下次 Lookup 重新读取成员名称（改名后立即生效）。
func (n *nameCache) Invalidate() {
	n.mu.Lock()
	n.at = time.Time{}
	n.mu.Unlock()
}

func (n *nameCache) Lookup(ctx context.Context) func(string) string {
	n.mu.Lock()
	if time.Since(n.at) > 10*time.Second {
		if devs, err := n.c.Devices(ctx); err == nil {
			n.m = map[string]string{}
			for _, d := range devs {
				n.m[d.DeviceID] = d.Name
				if len(d.DeviceID) >= 7 {
					n.m[d.DeviceID[:7]] = d.Name
				}
			}
			n.at = time.Now()
		}
	}
	m := n.m
	n.mu.Unlock()
	return func(id string) string {
		if v := m[id]; v != "" {
			return v
		}
		if len(id) > 7 {
			return id[:7]
		}
		return id
	}
}

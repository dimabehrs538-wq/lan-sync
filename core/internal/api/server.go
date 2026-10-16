// Package api 为 Tauri 前端提供本地 HTTP API（仅监听 127.0.0.1，Bearer token 鉴权，SSE 推送事件）。
package api

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/lan-sync/teamsync/core/internal/conflict"
	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
)

// UIEvent 是推给前端的简化事件，Message 为中文提示。
type UIEvent struct {
	Type    string    `json:"type"`
	Message string    `json:"message,omitempty"`
	Notify  bool      `json:"notify"`
	Time    time.Time `json:"time"`
	Data    any       `json:"data,omitempty"`
}

type Server struct {
	C           *stclient.Client
	Team        *team.Service
	Token       string
	EngineState func() string

	mu   sync.Mutex
	subs map[chan UIEvent]struct{}
}

func New(c *stclient.Client, token string, state func() string) *Server {
	return &Server{C: c, Team: &team.Service{C: c}, Token: token, EngineState: state, subs: map[chan UIEvent]struct{}{}}
}

func (s *Server) Publish(e UIEvent) {
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
	m.HandleFunc("GET /api/nearby", s.nearby)
	m.HandleFunc("POST /api/pending/devices/{id}/accept", s.acceptDevice)
	m.HandleFunc("DELETE /api/pending/devices/{id}", s.dismissDevice)
	m.HandleFunc("GET /api/pending/folders", s.pendingFolders)
	m.HandleFunc("POST /api/pending/folders/accept", s.acceptFolder)
	m.HandleFunc("GET /api/conflicts", s.conflicts)
	m.HandleFunc("POST /api/conflicts/resolve", s.resolveConflict)
	m.HandleFunc("GET /api/events", s.events)
	return s.cors(s.auth(m))
}

func (s *Server) cors(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
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
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
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

type folderView struct {
	stclient.Folder
	Status    *stclient.FolderStatus `json:"status,omitempty"`
	Conflicts int                    `json:"conflicts"`
}

func (s *Server) folders(w http.ResponseWriter, r *http.Request) {
	fs, err := s.C.Folders(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	out := make([]folderView, 0, len(fs))
	for _, f := range fs {
		v := folderView{Folder: f}
		if st, err := s.C.FolderStatus(r.Context(), f.ID); err == nil {
			v.Status = &st
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
	var in struct{ Label, Path string }
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	f, err := s.Team.CreateFolder(r.Context(), in.Label, in.Path)
	respond(w, f, err)
}

func (s *Server) scan(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, s.C.Scan(r.Context(), r.PathValue("id"), ""))
}

func (s *Server) pauseFolder(p bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond(w, nil, s.C.PatchFolder(r.Context(), r.PathValue("id"), map[string]any{"paused": p}))
	}
}

type deviceView struct {
	stclient.Device
	Connected bool   `json:"connected"`
	Address   string `json:"address,omitempty"`
	Self      bool   `json:"self"`
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
		out = append(out, deviceView{Device: d, Connected: c.Connected, Address: c.Address, Self: d.DeviceID == my})
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
	respond(w, nil, s.Team.AddMember(r.Context(), in.DeviceID, in.Name))
}

func (s *Server) delDevice(w http.ResponseWriter, r *http.Request) {
	respond(w, nil, s.C.DeleteDevice(r.Context(), r.PathValue("id")))
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

func (s *Server) pendingFolders(w http.ResponseWriter, r *http.Request) {
	p, err := s.C.PendingFolders(r.Context())
	respond(w, p, err)
}

func (s *Server) acceptFolder(w http.ResponseWriter, r *http.Request) {
	var in struct {
		FolderID string   `json:"folderID"`
		Label    string   `json:"label"`
		Path     string   `json:"path"`
		Devices  []string `json:"devices"`
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, 400, err)
		return
	}
	my, err := s.Team.MyID(r.Context())
	if err != nil {
		writeErr(w, 500, err)
		return
	}
	f := team.NewTeamFolder(in.Label, in.Path, append([]string{my}, in.Devices...))
	f.ID = in.FolderID
	respond(w, f, s.C.PutFolder(r.Context(), f))
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
	s.C.WatchEvents(ctx, func(e stclient.Event) {
		if ue, ok := Translate(e, names.Lookup(ctx)); ok {
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

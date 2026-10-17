package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
)

// 前端（tauri://localhost / http://tauri.localhost）跨域访问内核，所有用到的方法都必须通过预检。
func TestCORSPreflightAllowsAllMethods(t *testing.T) {
	s := New(stclient.New("http://127.0.0.1:1", "k"), "tok", func() string { return "running" }, nil)
	h := s.Handler()
	allowed := ""
	{
		req := httptest.NewRequest("OPTIONS", "/api/folders/x/members", nil)
		req.Header.Set("Origin", "http://tauri.localhost")
		req.Header.Set("Access-Control-Request-Method", "PUT")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 204 {
			t.Fatalf("preflight %d", rec.Code)
		}
		allowed = rec.Header().Get("Access-Control-Allow-Methods")
	}
	for _, m := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		if !strings.Contains(allowed, m) {
			t.Errorf("CORS 缺少方法 %s: %q", m, allowed)
		}
	}
}

// 取消勾选最后一位成员（members=[]）必须合法：文件夹变为仅本机。
func TestSetMembersEmptyList(t *testing.T) {
	var patched map[string]any
	st := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/system/status":
			w.Write([]byte(`{"myID":"ME"}`))
		case r.Method == "PATCH" && r.URL.Path == "/rest/config/folders/f1":
			b, _ := io.ReadAll(r.Body)
			json.Unmarshal(b, &patched)
		default:
			w.WriteHeader(404)
		}
	}))
	defer st.Close()
	s := New(stclient.New(st.URL, "k"), "tok", func() string { return "running" }, team.OpenStore(t.TempDir()+"/s.json", "/b"))
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	req, _ := http.NewRequest("PUT", srv.URL+"/api/folders/f1/members", strings.NewReader(`{"members":[]}`))
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(err, resp.StatusCode)
	}
	devs, _ := patched["devices"].([]any)
	if len(devs) != 1 || devs[0].(map[string]any)["deviceID"] != "ME" {
		t.Fatalf("expected only self, got %v", patched)
	}
}

func TestFriendlyError(t *testing.T) {
	cases := map[error]string{
		&stclient.APIError{Status: 404, Body: "no such folder"}:         "找不到",
		&stclient.APIError{Status: 400, Body: "parsing device ID: x"}:   "设备 ID",
		&stclient.APIError{Status: 500, Body: "boom"}:                   "同步引擎返回错误",
		errors.New("dial tcp 127.0.0.1:1: connect: connection refused"): "暂时不可用",
	}
	for e, want := range cases {
		if got := FriendlyError(e); !strings.Contains(got, want) {
			t.Errorf("%v -> %q, want %q", e, got, want)
		}
	}
}

func TestFriendlyErrorMarker(t *testing.T) {
	msg := FriendlyError(&stclient.APIError{Status: 500, Body: "folder marker missing (this indicates potential data loss, search docs/forum to get information about how to proceed)"})
	if strings.Contains(msg, "missing") || !strings.Contains(msg, ".stfolder") {
		t.Fatal(msg)
	}
}

package team

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func TestRemoveFolder(t *testing.T) {
	deleted, patches := false, 0
	ReconnectPause = 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/rest/config/folders/f1":
			w.Write([]byte(`{"id":"f1","path":"/data/f1","devices":[{"deviceID":"ME"},{"deviceID":"PEER"}]}`))
		case r.URL.Path == "/rest/system/status":
			w.Write([]byte(`{"myID":"ME"}`))
		case r.Method == "DELETE" && r.URL.Path == "/rest/config/folders/f1":
			deleted = true
		case r.Method == "PATCH" && r.URL.Path == "/rest/config/devices/PEER":
			patches++
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	s := &Service{C: stclient.New(srv.URL, "k")}
	st := OpenStore(filepath.Join(t.TempDir(), "s.json"), "/base")
	var trashedPath string
	fakeTrash := func(p string, perm bool) (bool, error) { trashedPath = p; return true, nil }

	// 仅停止同步：不删文件
	res, err := s.RemoveFolder(context.Background(), st, "f1", false, false, fakeTrash)
	if err != nil || !deleted || res.FilesDeleted || trashedPath != "" {
		t.Fatalf("%+v %v deleted=%v trash=%q", res, err, deleted, trashedPath)
	}
	if patches != 2 {
		t.Fatalf("expected pause+resume, got %d", patches)
	}
	// 移除后：共享者的邀请被隐藏，不会立刻再出现；本机不记录自己
	if !st.IsIgnored("f1", "PEER") || st.IsIgnored("f1", "ME") {
		t.Fatal("peer offer should be suppressed")
	}
	if inv := FilterInvitations(pending("f1", "PEER", "x", "t"), nil, nil, st, nil); len(inv) != 0 {
		t.Fatal("should not resurface")
	}
	// 对方重新邀请（撤回事件）后重新出现
	st.UnignorePair("f1", "PEER")
	if inv := FilterInvitations(pending("f1", "PEER", "x", "t2"), nil, nil, st, nil); len(inv) != 1 {
		t.Fatal("re-invite should show")
	}
	// 同时删除文件
	res, err = s.RemoveFolder(context.Background(), st, "f1", true, false, fakeTrash)
	if err != nil || !res.FilesDeleted || !res.Trashed || trashedPath != "/data/f1" {
		t.Fatalf("%+v %v", res, err)
	}
}

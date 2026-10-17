package team

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func fakeST(t *testing.T, devs *[]stclient.Device, patches *[]string) *Service {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/system/status":
			w.Write([]byte(`{"myID":"ME"}`))
		case r.Method == "GET" && r.URL.Path == "/rest/config/devices":
			json.NewEncoder(w).Encode(*devs)
		case r.Method == "PATCH":
			var m map[string]any
			json.NewDecoder(r.Body).Decode(&m)
			id := filepath.Base(r.URL.Path)
			b, _ := json.Marshal(m)
			*patches = append(*patches, id+" "+string(b))
			for i := range *devs {
				if (*devs)[i].DeviceID == id {
					if n, ok := m["name"].(string); ok {
						(*devs)[i].Name = n
					}
				}
			}
		}
	}))
	t.Cleanup(srv.Close)
	return &Service{C: stclient.New(srv.URL, "k")}
}

func TestAnnouncedNames(t *testing.T) {
	devs := []stclient.Device{{DeviceID: "ME", Name: "me"}, {DeviceID: "A", Name: "旧名"}, {DeviceID: "B", Name: "我的备注"}}
	var patches []string
	s := fakeST(t, &devs, &patches)
	ctx := context.Background()
	st := OpenStore(filepath.Join(t.TempDir(), "s.json"), "/b")
	// 升级迁移：已有名称视为备注
	if err := s.MigrateRemarks(ctx, st); err != nil || !st.HasRemark("A") || !st.HasRemark("B") || st.HasRemark("ME") {
		t.Fatal("migrate", err)
	}
	st.SetRemark("A", false)
	// 没有备注：采用对方通告的名称
	if ch, err := s.ApplyAnnounced(ctx, st, "A", "张三"); err != nil || !ch || devs[1].Name != "张三" {
		t.Fatal(ch, err, devs)
	}
	// 有备注：备注优先，但记录通告名
	if ch, _ := s.ApplyAnnounced(ctx, st, "B", "李四"); ch || devs[2].Name != "我的备注" || st.Announced("B") != "李四" {
		t.Fatal(devs)
	}
	// 清除备注后显示对方的名称
	if n, err := s.ClearRemark(ctx, st, "B"); err != nil || n != "李四" || devs[2].Name != "李四" || st.HasRemark("B") {
		t.Fatal(n, err)
	}
	// 迁移只做一次
	st.SetRemark("A", false)
	s.MigrateRemarks(ctx, st)
	if st.HasRemark("A") {
		t.Fatal("migrated twice")
	}
}

func TestSetMyName(t *testing.T) {
	ReconnectPause = 0
	devs := []stclient.Device{{DeviceID: "ME", Name: "host"}, {DeviceID: "A"}, {DeviceID: "P", Paused: true}}
	var patches []string
	s := fakeST(t, &devs, &patches)
	n, err := s.SetMyName(context.Background(), "  王五 ")
	want := []string{`ME {"name":"王五"}`, `A {"paused":true}`, `A {"paused":false}`}
	if err != nil || n != "王五" || len(patches) != 3 || patches[0] != want[0] || patches[1] != want[1] || patches[2] != want[2] {
		t.Fatal(n, err, patches)
	}
	if _, err := s.SetMyName(context.Background(), ""); err == nil {
		t.Fatal("empty")
	}
}

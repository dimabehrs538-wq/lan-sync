package team

import (
	"path/filepath"
	"testing"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func pending(fid, dev, label, t string) map[string]stclient.PendingFolder {
	p := stclient.PendingFolder{OfferedBy: map[string]struct {
		Time  string `json:"time"`
		Label string `json:"label"`
	}{}}
	p.OfferedBy[dev] = struct {
		Time  string `json:"time"`
		Label string `json:"label"`
	}{t, label}
	return map[string]stclient.PendingFolder{fid: p}
}

func TestInvitationsIgnoreAndReinvite(t *testing.T) {
	dir := t.TempDir()
	st := OpenStore(filepath.Join(dir, "s.json"), filepath.Join(dir, "base"))
	p := pending("f1", "DEV", "设计:文档", "2026-10-16T10:00:00Z")
	inv := FilterInvitations(p, nil, map[string]string{"DEV": "张三"}, st)
	if len(inv) != 1 || inv[0].DeviceName != "张三" || inv[0].SuggestPath != filepath.Join(dir, "base", "设计_文档") {
		t.Fatalf("%+v", inv)
	}
	if err := st.Ignore("f1", "DEV", inv[0].OfferTime); err != nil {
		t.Fatal(err)
	}
	if len(FilterInvitations(p, nil, nil, st)) != 0 {
		t.Fatal("ignored invite should be hidden")
	}
	// 持久化
	st2 := OpenStore(filepath.Join(dir, "s.json"), "x")
	if len(FilterInvitations(p, nil, nil, st2)) != 0 || st2.Get().DefaultBaseDir != filepath.Join(dir, "base") {
		t.Fatal("not persisted")
	}
	// 重新邀请 = 新的邀请时间 -> 重新出现
	p2 := pending("f1", "DEV", "设计:文档", "2026-10-16T11:00:00Z")
	if len(FilterInvitations(p2, nil, nil, st2)) != 1 {
		t.Fatal("re-invite should resurface")
	}
	// 已有的文件夹不算邀请
	if len(FilterInvitations(p2, map[string]bool{"f1": true}, nil, st2)) != 0 {
		t.Fatal("existing folder")
	}
}

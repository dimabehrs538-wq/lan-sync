package team

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func TestLabelMarker(t *testing.T) {
	if l := MakeLabel("设计文档", true); l != "设计文档 [只读]" {
		t.Fatal(l)
	}
	if l := MakeLabel("设计文档 [只读]", false); l != "设计文档" {
		t.Fatal(l)
	}
	if l := MakeLabel("设计文档 [只读]", true); l != "设计文档 [只读]" {
		t.Fatal("不应重复添加标记", l)
	}
	if n, ro := ParseLabel("周报 [只读]"); n != "周报" || !ro {
		t.Fatal(n, ro)
	}
	if n, ro := ParseLabel("周报"); n != "周报" || ro {
		t.Fatal(n, ro)
	}
}

func TestModeFileAndFolderMode(t *testing.T) {
	d := t.TempDir()
	if _, ok := ReadMode(d); ok {
		t.Fatal("无文件时应为未设置")
	}
	if err := WriteMode(d, ShareMode{Mode: ModeReadOnly, Owner: "OWNER"}); err != nil {
		t.Fatal(err)
	}
	m, ok := ReadMode(d)
	if !ok || m.Mode != ModeReadOnly || m.Owner != "OWNER" {
		t.Fatalf("%+v", m)
	}
	os.WriteFile(filepath.Join(d, ModeFile), []byte(`{"mode":"weird"}`), 0o644)
	if _, ok := ReadMode(d); ok {
		t.Fatal("未知模式应忽略")
	}
	cases := []struct {
		typ, mode, role string
	}{{"sendonly", ModeReadOnly, "owner"}, {"receiveonly", ModeReadOnly, "member"}, {"sendreceive", ModeCollab, ""}}
	for _, c := range cases {
		if m, r := FolderMode(stclient.Folder{Type: c.typ, Path: t.TempDir()}, "ME"); m != c.mode || r != c.role {
			t.Errorf("%s: %s/%s", c.typ, m, r)
		}
	}
}

func TestInvitationCarriesMode(t *testing.T) {
	st := OpenStore(filepath.Join(t.TempDir(), "s.json"), t.TempDir())
	p := map[string]stclient.PendingFolder{}
	pf := stclient.PendingFolder{}
	pf.OfferedBy = map[string]struct {
		Time  string `json:"time"`
		Label string `json:"label"`
	}{"DEV": {Time: time.Now().Format(time.RFC3339), Label: "资料 [只读]"}}
	p["f1"] = pf
	inv := FilterInvitations(p, nil, nil, st, nil)
	if len(inv) != 1 || inv[0].Label != "资料" || inv[0].Mode != ModeReadOnly || filepath.Base(inv[0].SuggestPath) != "资料" {
		t.Fatalf("%+v", inv)
	}
}

func TestVersionFileAndList(t *testing.T) {
	d := t.TempDir()
	f := stclient.Folder{Path: d}
	vt := time.Date(2026, 10, 19, 9, 30, 5, 0, time.Local)
	p := VersionFile(f, "docs/方案.v2.md", vt)
	want := filepath.Join(d, ".stversions", "docs", "方案.v2~20261019-093005.md")
	if p != want {
		t.Fatalf("%s != %s", p, want)
	}
	if p := VersionFile(f, ".env", vt); filepath.Base(p) != ".env~20261019-093005" {
		t.Fatal(p)
	}
	os.MkdirAll(filepath.Dir(want), 0o755)
	os.WriteFile(want, []byte("old"), 0o644)
	os.MkdirAll(filepath.Join(d, "docs"), 0o755)
	os.WriteFile(filepath.Join(d, "keep.md"), []byte("x"), 0o644)
	raw := map[string][]stclient.FileVersion{
		"docs/方案.v2.md": {{VersionTime: vt.Add(-time.Hour), Size: 1}, {VersionTime: vt, Size: 3}},
		"keep.md":       {{VersionTime: vt.Add(-2 * time.Hour), Size: 1}},
	}
	l := BuildVersionList(f, raw)
	if len(l) != 2 || l[0].Path != "docs/方案.v2.md" || !l[0].Deleted || l[1].Deleted {
		t.Fatalf("%+v", l)
	}
	if l[0].Versions[0].VersionTime != vt || l[0].Versions[0].File != want || l[0].Versions[1].File != "" {
		t.Fatalf("版本排序或文件路径错误: %+v", l[0].Versions)
	}
}

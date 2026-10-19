package team

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func writeVersion(t *testing.T, dir, rel string, at time.Time, size int) string {
	t.Helper()
	ext := filepath.Ext(rel)
	p := filepath.Join(dir, filepath.FromSlash(strings.TrimSuffix(rel, ext)+"~"+at.Local().Format("20060102-150405")+ext))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func TestRetentionValidate(t *testing.T) {
	if err := DefaultRetention.Validate(); err != nil {
		t.Fatal(err)
	}
	for _, r := range []Retention{{0, 30, 10}, {101, 30, 10}, {10, 0, 10}, {10, 366, 10}, {10, 30, 0}, {10, 30, MaxMaxMB + 1}} {
		if r.Validate() == nil {
			t.Fatalf("%+v 应无效", r)
		}
	}
	if (Retention{1, 365, 1}).Validate() != nil || (Retention{100, 1, MaxMaxMB}).Validate() != nil {
		t.Fatal("边界值应有效")
	}
}

func TestStoreRetention(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	st := OpenStore(p, t.TempDir())
	if r, c := st.FolderRetention("F"); r != DefaultRetention || c {
		t.Fatal(r, c)
	}
	if err := st.SetDefaultRetention(Retention{Keep: 0, Days: 1, MaxMB: 1}); err == nil {
		t.Fatal("无效值应拒绝")
	}
	if err := st.SetDefaultRetention(Retention{5, 7, 100}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetFolderRetention("G", &Retention{3, 2, 50}); err != nil {
		t.Fatal(err)
	}
	st2 := OpenStore(p, "") // 持久化
	if r, c := st2.FolderRetention("F"); r != (Retention{5, 7, 100}) || c {
		t.Fatal("默认值应生效", r, c)
	}
	if r, c := st2.FolderRetention("G"); r != (Retention{3, 2, 50}) || !c {
		t.Fatal(r, c)
	}
	_ = st2.SetFolderRetention("G", nil)
	if _, c := st2.FolderRetention("G"); c {
		t.Fatal("应改回默认")
	}
}

func TestRetentionPatch(t *testing.T) {
	f := stclient.Folder{Versioning: DefaultVersioning()}
	if _, ok := RetentionPatch(f, DefaultRetention); ok {
		t.Fatal("一致时不应修改")
	}
	f.Versioning.Params["fsPath"] = "D:/v"
	v, ok := RetentionPatch(f, Retention{5, 7, 1})
	if !ok || v.Params["keep"] != "5" || v.Params["cleanoutDays"] != "7" || v.Params["fsPath"] != "D:/v" || v.Type != "simple" {
		t.Fatalf("%v %+v", ok, v)
	}
	if f.Versioning.Params["keep"] != "10" {
		t.Fatal("不应修改原配置")
	}
	if _, ok := RetentionPatch(stclient.Folder{}, DefaultRetention); ok {
		t.Fatal("未开启删除保护时不应修改")
	}
}

func TestPruneVersions(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".stversions")
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.Local)
	day := 24 * time.Hour
	old := writeVersion(t, dir, "a/旧.txt", now.Add(-40*day), 10) // 超过 30 天
	var a []string
	for i := 0; i < 4; i++ { // 4 份，保留 2 份
		a = append(a, writeVersion(t, dir, "a/文档.md", now.Add(-time.Duration(4-i)*time.Hour), 100))
	}
	b1 := writeVersion(t, dir, "b~x.txt", now.Add(-10*day), 1<<20) // 文件名含 ~
	b2 := writeVersion(t, dir, "b~x.txt", now.Add(-1*day), 1<<20)
	c := writeVersion(t, dir, ".env", now.Add(-2*day), 5)

	res, err := PruneVersions(dir, Retention{Keep: 2, Days: 30, MaxMB: 1024}, now)
	if err != nil {
		t.Fatal(err)
	}
	if exists(old) || exists(a[0]) || exists(a[1]) || !exists(a[2]) || !exists(a[3]) || !exists(b1) || !exists(b2) || !exists(c) {
		t.Fatalf("份数/天数清理不对: %+v", res)
	}
	if res.Removed != 3 || res.Files != 5 || res.Freed != 210 {
		t.Fatalf("%+v", res)
	}
	if exists(filepath.Join(dir, "a")) == false {
		t.Fatal("非空目录不应删除")
	}
	// 容量上限 1 MB（总计 2 MB+205 B）：从最旧的开始删 b1(10 天前) → c(2 天前) → b2(1 天前)，直到 ≤ 1 MB
	res, _ = PruneVersions(dir, Retention{Keep: 10, Days: 365, MaxMB: 1}, now)
	if exists(b1) || exists(b2) || exists(c) || !exists(a[2]) || !exists(a[3]) {
		t.Fatalf("容量清理不对: %+v", res)
	}
	if res.Size != 200 || res.Files != 2 || res.Removed != 3 {
		t.Fatalf("%+v", res)
	}
	// 天数缩短到 1 天之外的全部删除后，空目录应被删除
	writeVersion(t, dir, "x/y/z.txt", now.Add(-3*day), 1)
	res, _ = PruneVersions(dir, Retention{Keep: 10, Days: 1, MaxMB: 1}, now)
	if exists(filepath.Join(dir, "x")) || res.Files != 2 {
		t.Fatalf("空目录应删除: %+v", res)
	}
	if !exists(dir) {
		t.Fatal("根目录应保留")
	}
	if n, files := VersionsSize(dir); n != 200 || files != 2 {
		t.Fatal(n, files)
	}
	// 目录不存在
	if res, err := PruneVersions(filepath.Join(t.TempDir(), "none"), DefaultRetention, now); err != nil || res.Files != 0 {
		t.Fatal(res, err)
	}
}

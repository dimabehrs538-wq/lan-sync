package team

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func TestCheckWindowsStyle(t *testing.T) {
	r := PathRules{Win: true, Home: `C:\Users\张三`, BaseDir: `D:\TeamSync`, Existing: []stclient.Folder{
		{ID: "a", Label: "设计文档", Path: `D:\TeamSync\设计文档`},
		{ID: "b", Label: "商城", Path: `E:\Work\Mall\`},
	}}
	bad := map[string]string{
		`D:\TeamSync\设计文档`:             "已被团队文件夹「设计文档」使用",
		`d:/teamsync/设计文档/`:            "已被团队文件夹「设计文档」使用", // 大小写、斜杠、末尾斜杠
		`D:\TeamSync\.\x\..\设计文档`:      "已被团队文件夹「设计文档」使用",
		`D:\TeamSync\设计文档\子目录`:         "位于团队文件夹「设计文档」之内",
		`E:\WORK\mall\docs`:            "位于团队文件夹「商城」之内",
		`E:\Work`:                      "包含了团队文件夹「商城」",
		`D:\TeamSync`:                  "默认位置",
		`d:\teamsync\`:                 "默认位置",
		`D:\`:                          "磁盘根目录",
		`D:`:                           "磁盘根目录",
		`\\nas\share`:                  "磁盘根目录",
		`C:\Users\张三`:                  "用户主目录",
		`c:\users\张三\desktop`:          "桌面",
		`C:\Users\张三\OneDrive\Desktop`: "桌面",
		`C:\Users\张三\Downloads\`:       "下载",
		``:                             "不能为空",
	}
	for p, want := range bad {
		err := r.Check(p, nil)
		if err == nil || !strings.Contains(err.Error(), want) || (p != "" && !errors.Is(err, ErrPathNotAllowed)) {
			t.Errorf("%q: 期望包含 %q，实际 %v", p, want, err)
		}
	}
	good := []string{`D:\TeamSync\商城方案`, `D:\TeamSync\设计文档 (2)`, `D:\TeamSync\设计文档2`, `E:\Work\Mall2`, `C:\Users\张三\Downloads\资料`, `C:\Users\张三\Desktop\项目`, `\\nas\share\团队`}
	for _, p := range good {
		if err := r.Check(p, nil); err != nil {
			t.Errorf("%q 应允许: %v", p, err)
		}
	}
	// 修改自身位置时排除自己（同一路径也允许，例如重新指向）
	r.ExcludeID = "a"
	if err := r.Check(`D:\TeamSync\设计文档`, nil); err != nil {
		t.Errorf("排除自身后应允许: %v", err)
	}
}

func TestCheckUnixCaseSensitive(t *testing.T) {
	r := PathRules{Win: false, Home: "/home/z", Existing: []stclient.Folder{{ID: "a", Path: "/data/Docs"}}}
	if err := r.Check("/data/docs", nil); err != nil {
		t.Errorf("Linux 区分大小写，/data/docs 与 /data/Docs 不同: %v", err)
	}
	for _, p := range []string{"/", "/home/z", "/home/z/Desktop", "/data", "/data/Docs/x", "/data/Docs/../Docs"} {
		if r.Check(p, nil) == nil {
			t.Errorf("%q 应被拒绝", p)
		}
	}
}

func TestCheckSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	os.MkdirAll(real, 0o755)
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("不支持符号链接")
	}
	r := PathRules{Existing: []stclient.Folder{{ID: "a", Label: "A", Path: real}}}
	if err := r.Check(filepath.Join(link, "sub", "new"), ResolvePath); err == nil || !strings.Contains(err.Error(), "之内") {
		t.Fatalf("通过符号链接嵌套应被识别: %v（real=%s link=%s）", err, ResolvePath(real), ResolvePath(filepath.Join(link, "sub", "new")))
	}
}

func TestFindOverlaps(t *testing.T) {
	fs := []stclient.Folder{
		{ID: "a", Label: "甲", Path: `D:\TeamSync`},
		{ID: "b", Label: "乙", Path: `d:\teamsync\`},
		{ID: "c", Label: "丙", Path: `D:\TeamSync\丙`},
		{ID: "d", Label: "丁", Path: `E:\丁`},
		{ID: "e", Label: "戊", Path: `E:\`},
	}
	ov := FindOverlaps(fs, true, nil, nil)
	if len(ov) != 3 || ov["b"].Kind != "same" || ov["b"].With != "a" || ov["c"].Kind != "inside" || ov["e"].Kind != "contains" || ov["e"].With != "d" {
		t.Fatalf("重叠识别错误: %+v", ov)
	}
	if _, ok := ov["a"]; ok {
		t.Fatal("先添加的文件夹不应被标记")
	}
	if !strings.Contains(OverlapMessage(ov["b"]), "同一个位置") {
		t.Fatal(OverlapMessage(ov["b"]))
	}
}

func TestSafeNameWindows(t *testing.T) {
	cases := map[string]string{
		`设计:文档?`: "设计_文档_", "CON": "_CON", "nul.txt": "_nul.txt", "报告. ": "报告", "..": "folder", "": "folder", " a/b\\c ": "a_b_c", "com1": "_com1", "console": "console",
	}
	for in, want := range cases {
		if got := SafeName(in); got != want {
			t.Errorf("SafeName(%q)=%q 期望 %q", in, got, want)
		}
	}
}

func TestSuggestPath(t *testing.T) {
	base := t.TempDir()
	if p := SuggestPath(base, "设计文档", nil); p != filepath.Join(base, "设计文档") {
		t.Fatalf("默认应为 base/名称: %s", p)
	}
	// 已存在且为空：可直接使用
	os.MkdirAll(filepath.Join(base, "空"), 0o755)
	if p := SuggestPath(base, "空", nil); p != filepath.Join(base, "空") {
		t.Fatalf("空目录可复用: %s", p)
	}
	// 已存在且非空 -> (2)
	os.MkdirAll(filepath.Join(base, "设计文档"), 0o755)
	os.WriteFile(filepath.Join(base, "设计文档", "a.md"), []byte("x"), 0o644)
	if p := SuggestPath(base, "设计文档", nil); p != filepath.Join(base, "设计文档 (2)") {
		t.Fatalf("非空应追加 (2): %s", p)
	}
	// 被已有文件夹使用 -> 跳过
	ex := []stclient.Folder{{ID: "x", Path: filepath.Join(base, "设计文档 (2)")}}
	if p := SuggestPath(base, "设计文档", ex); p != filepath.Join(base, "设计文档 (3)") {
		t.Fatalf("被占用应追加 (3): %s", p)
	}
	// 永远不是 base 本身
	if p := SuggestPath(base, "", nil); p == base {
		t.Fatal("不应使用 base 本身")
	}
}

func TestInspectPath(t *testing.T) {
	d := t.TempDir()
	if i := InspectPath(filepath.Join(d, "none")); i.Exists || !i.Empty {
		t.Fatalf("%+v", i)
	}
	os.MkdirAll(filepath.Join(d, ".stfolder"), 0o755)
	if i := InspectPath(d); !i.Exists || !i.Empty {
		t.Fatalf("只有 .stfolder 视为空: %+v", i)
	}
	os.WriteFile(filepath.Join(d, "a"), nil, 0o644)
	if i := InspectPath(d); i.Empty || i.Entries != 1 {
		t.Fatalf("%+v", i)
	}
}

func TestFindOverlapsByAddedTime(t *testing.T) {
	// Syncthing 配置按 ID 排序，不能代表添加顺序：以记录的添加时间为准
	now := time.Now()
	fs := []stclient.Folder{{ID: "a", Path: "/x/docs/sub"}, {ID: "z", Path: "/x/docs"}}
	at := map[string]time.Time{"z": now, "a": now.Add(time.Hour)}
	ov := FindOverlaps(fs, false, nil, func(f stclient.Folder) time.Time { return at[f.ID] })
	if len(ov) != 1 || ov["a"].With != "z" || ov["a"].Kind != "inside" {
		t.Fatalf("应标记后添加的 a: %+v", ov)
	}
}

package team

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func TestDiagnose(t *testing.T) {
	dir := t.TempDir()
	f := stclient.Folder{ID: "f", Path: dir}
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("x"), 0o644)
	os.Mkdir(filepath.Join(dir, ".stfolder"), 0o755)
	if p := Diagnose(f, ""); p != nil {
		t.Fatalf("healthy folder: %+v", p)
	}
	// 标记被删除（git clean 等）
	os.Remove(filepath.Join(dir, ".stfolder"))
	p := Diagnose(f, "folder marker missing (this indicates potential data loss, search docs/forum to get information about how to proceed)")
	if p == nil || p.Kind != "markerMissing" || !p.PathExists || p.PathEmpty || p.InGitRepo || !strings.Contains(p.Message, "修复") {
		t.Fatalf("%+v", p)
	}
	// 引擎尚未报错时也能从本地检测到
	if p := Diagnose(f, ""); p == nil || p.Kind != "markerMissing" {
		t.Fatalf("local detect: %+v", p)
	}
	// git 仓库内
	os.Mkdir(filepath.Join(dir, ".git"), 0o755)
	sub := filepath.Join(dir, "docs")
	os.Mkdir(sub, 0o755)
	if p := Diagnose(stclient.Folder{Path: sub}, ""); p == nil || !p.InGitRepo || !p.PathEmpty {
		t.Fatalf("git: %+v", p)
	}
	// 路径不存在
	if p := Diagnose(stclient.Folder{Path: filepath.Join(dir, "gone")}, "folder path missing"); p == nil || p.Kind != "pathMissing" {
		t.Fatalf("missing: %+v", p)
	}
	// 暂停的文件夹不诊断
	if p := Diagnose(stclient.Folder{Path: filepath.Join(dir, "gone"), Paused: true}, ""); p != nil {
		t.Fatal("paused")
	}
}

func TestTranslateEngineError(t *testing.T) {
	for _, in := range []string{"folder marker missing (this indicates potential data loss…)", "folder path missing"} {
		if out := TranslateEngineError(in); strings.Contains(out, "missing") {
			t.Errorf("not translated: %q", out)
		}
	}
}

func TestRepair(t *testing.T) {
	dir := t.TempDir()
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/rest/config/folders/f1":
			json.NewEncoder(w).Encode(stclient.Folder{ID: "f1", Path: dir})
		default:
			calls = append(calls, r.Method+" "+r.URL.Path)
		}
	}))
	defer srv.Close()
	s := &Service{C: stclient.New(srv.URL, "k")}
	ctx := context.Background()
	// 空目录：需要确认
	if err := s.Repair(ctx, "f1", false); !errors.Is(err, ErrNeedConfirm) {
		t.Fatalf("want confirm, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".stfolder")); err == nil {
		t.Fatal("must not create marker without confirm")
	}
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("x"), 0o644)
	if err := s.Repair(ctx, "f1", false); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, ".stfolder")); err != nil || !fi.IsDir() {
		t.Fatal("marker not created")
	}
	if strings.Join(calls, ",") != "PATCH /rest/config/folders/f1,PATCH /rest/config/folders/f1,POST /rest/db/scan" {
		t.Fatalf("calls %v", calls)
	}
	// 路径不存在
	os.RemoveAll(dir)
	if err := s.Repair(ctx, "f1", true); !errors.Is(err, ErrPathMissing) {
		t.Fatalf("want path missing, got %v", err)
	}
}

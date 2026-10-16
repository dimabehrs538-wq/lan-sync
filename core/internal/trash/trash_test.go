//go:build !windows

package trash

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemoveToTrash(t *testing.T) {
	root := t.TempDir()
	TrashDir = func() (string, error) { return filepath.Join(root, "Trash"), nil }
	d := filepath.Join(root, "设计文档")
	os.MkdirAll(filepath.Join(d, "sub"), 0o755)
	os.WriteFile(filepath.Join(d, "sub", "a.md"), []byte("x"), 0o644)
	trashed, err := Remove(d, false)
	if err != nil || !trashed {
		t.Fatal(trashed, err)
	}
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Fatal("still exists")
	}
	if _, err := os.Stat(filepath.Join(root, "Trash", "files", "设计文档", "sub", "a.md")); err != nil && filepath.Base(os.Getenv("HOME")) != "" {
		// macOS 没有 files 子目录
		if _, err2 := os.Stat(filepath.Join(root, "Trash", "设计文档")); err2 != nil {
			t.Fatal(err)
		}
	}
	// 同名再删一次不覆盖
	os.MkdirAll(d, 0o755)
	if ok, err := Remove(d, false); err != nil || !ok {
		t.Fatal(err)
	}
	// 不存在不报错
	if ok, err := Remove(d, false); err != nil || ok {
		t.Fatal(ok, err)
	}
}

func TestPermanentFallback(t *testing.T) {
	root := t.TempDir()
	TrashDir = func() (string, error) { return "/proc/不可写", nil }
	d := filepath.Join(root, "x")
	os.MkdirAll(d, 0o755)
	if _, err := Remove(d, false); err == nil {
		t.Fatal("expected error without permanent")
	}
	ok, err := Remove(d, true)
	if err != nil || ok {
		t.Fatal(ok, err)
	}
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Fatal("not removed")
	}
}

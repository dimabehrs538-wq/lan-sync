//go:build !windows

package trash

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestRemoveToTrash(t *testing.T) {
	SystemTrashCLI = "" // 不碰运行测试的机器的真实废纸篓
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
	SystemTrashCLI = ""
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

func TestSystemTrashCLI(t *testing.T) {
	root := t.TempDir()
	TrashDir = func() (string, error) { return filepath.Join(root, "Trash"), nil }
	defer func() { SystemTrashCLI = "" }()
	// 模拟 macOS 的 /usr/bin/trash：移到 root/sys
	cli := filepath.Join(root, "trash.sh")
	os.MkdirAll(filepath.Join(root, "sys"), 0o755)
	os.WriteFile(cli, []byte("#!/bin/sh\nmv \"$1\" \""+filepath.Join(root, "sys")+"/\"\n"), 0o755)
	SystemTrashCLI = cli
	d := filepath.Join(root, "a")
	os.MkdirAll(d, 0o755)
	if ok, err := Remove(d, false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := os.Stat(filepath.Join(root, "sys", "a")); err != nil {
		t.Fatal("系统废纸篓命令未被使用", err)
	}
	// 命令失败时退回到直接移动
	os.WriteFile(cli, []byte("#!/bin/sh\nexit 1\n"), 0o755)
	os.MkdirAll(d, 0o755)
	if ok, err := Remove(d, false); err != nil || !ok {
		t.Fatal(ok, err)
	}
	if _, err := os.Stat(d); !os.IsNotExist(err) {
		t.Fatal("still exists")
	}
}

func TestUnreadableTrashNoInfiniteLoop(t *testing.T) {
	SystemTrashCLI = ""
	if os.Geteuid() == 0 {
		t.Skip("root 可读任何目录")
	}
	root := t.TempDir()
	tr := filepath.Join(root, "Trash")
	os.MkdirAll(filepath.Join(tr, "files"), 0o755)
	os.MkdirAll(filepath.Join(tr, "info"), 0o755)
	TrashDir = func() (string, error) { return tr, nil }
	locked := filepath.Join(tr, "files")
	if runtime.GOOS == "darwin" {
		locked = tr // macOS 直接放在 ~/.Trash 下
	}
	os.Chmod(locked, 0o200) // 无法查找：模拟受隐私保护的 ~/.Trash（EPERM）
	defer os.Chmod(locked, 0o755)
	d := filepath.Join(root, "b")
	os.MkdirAll(d, 0o755)
	done := make(chan struct{})
	var err error
	go func() { _, err = Remove(d, false); close(done) }()
	select {
	case <-done:
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("hang")
	}
}

package conflict

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	orig, tm, dev, ok := Parse("设计.sync-conflict-20261016-153012-ABCDEF1.md")
	if !ok || orig != "设计.md" || dev != "ABCDEF1" || tm.Hour() != 15 || tm.Minute() != 30 {
		t.Fatalf("got %q %v %q %v", orig, tm, dev, ok)
	}
	if o, _, _, ok := Parse("Makefile.sync-conflict-20261016-153012-ABCDEF1"); !ok || o != "Makefile" {
		t.Fatalf("no ext: %q %v", o, ok)
	}
	if _, _, _, ok := Parse("普通文件.md"); ok {
		t.Fatal("should not parse")
	}
}

func TestScanResolve(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "sub"), 0o755)
	os.MkdirAll(filepath.Join(root, ".stversions"), 0o755)
	write := func(p, s string) { os.WriteFile(filepath.Join(root, p), []byte(s), 0o644) }
	write("sub/a.md", "orig")
	write("sub/a.sync-conflict-20261016-101010-AAAAAAA.md", "theirs")
	write("b.txt.sync-conflict-20261016-101010-BBBBBBB.txt", "x")
	write(".stversions/a.sync-conflict-20261016-101010-CCCCCCC.md", "ignored")
	cs, err := Scan(root)
	if err != nil || len(cs) != 2 {
		t.Fatalf("scan: %v %d", err, len(cs))
	}
	var a Conflict
	for _, c := range cs {
		if c.Device == "AAAAAAA" {
			a = c
		}
	}
	if a.Original != filepath.Join(root, "sub", "a.md") {
		t.Fatalf("original %s", a.Original)
	}
	if err := Resolve(root, a.Path, true); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(a.Original); string(b) != "theirs" {
		t.Fatalf("content %s", b)
	}
	if err := Resolve(root, "/etc/passwd.sync-conflict-20261016-101010-AAAAAAA", false); err == nil {
		t.Fatal("expected outside-root error")
	}
	for _, c := range cs {
		if c.Device == "BBBBBBB" {
			if err := Resolve(root, c.Path, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	if cs, _ := Scan(root); len(cs) != 0 {
		t.Fatalf("remaining %d", len(cs))
	}
}

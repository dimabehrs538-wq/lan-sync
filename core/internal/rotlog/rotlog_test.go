package rotlog

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRotationBounded(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	w, err := Open(p, 1000, 2)
	if err != nil {
		t.Fatal(err)
	}
	line := bytes.Repeat([]byte("a"), 99)
	line = append(line, '\n')
	for i := 0; i < 500; i++ {
		if _, err := w.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	w.Close()
	var total int64
	for _, f := range []string{p, p + ".1", p + ".2"} {
		st, err := os.Stat(f)
		if err != nil {
			t.Fatalf("%s 应存在: %v", f, err)
		}
		if st.Size() > 1000 {
			t.Fatalf("%s 超过上限: %d", f, st.Size())
		}
		total += st.Size()
	}
	if _, err := os.Stat(p + ".3"); !os.IsNotExist(err) {
		t.Fatal("不应保留第 3 个旧文件")
	}
	if total > 3000 {
		t.Fatalf("总大小超限: %d", total)
	}
}

func TestOversizedOnOpen(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.log")
	os.WriteFile(p, bytes.Repeat([]byte("b"), 5000), 0o600)
	w, err := Open(p, 1000, 1)
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("hi\n"))
	w.Close()
	st, _ := os.Stat(p)
	if st.Size() != 3 {
		t.Fatalf("启动时应轮换超限日志，当前大小 %d", st.Size())
	}
}

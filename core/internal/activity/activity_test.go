package activity

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestCapsAndPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "activity.json")
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	l := Open(p)
	l.Now = func() time.Time { return now }
	l.Add(Item{Type: "x", Message: "old", Time: now.Add(-8 * 24 * time.Hour)}, 0)
	for i := 0; i < 600; i++ {
		l.Add(Item{Type: "x", Message: fmt.Sprint(i), Time: now.Add(time.Duration(i) * time.Second)}, 0)
	}
	got := l.List()
	if len(got) != 500 {
		t.Fatalf("应最多 500 条，实际 %d", len(got))
	}
	if got[0].Message != "599" {
		t.Fatalf("最新应在前: %s", got[0].Message)
	}
	if err := l.Flush(); err != nil {
		t.Fatal(err)
	}
	l2 := Open(p)
	l2.Now = func() time.Time { return now }
	if n := len(l2.List()); n != 500 {
		t.Fatalf("重新打开后应为 500 条，实际 %d", n)
	}
	// 新 ID 不与旧记录冲突
	it := l2.Add(Item{Message: "new"}, 0)
	if it.ID <= got[0].ID {
		t.Fatalf("ID 应递增: %d <= %d", it.ID, got[0].ID)
	}
}

func TestMaxAge(t *testing.T) {
	now := time.Now()
	l := Open("")
	l.Add(Item{Message: "a", Time: now.Add(-7*24*time.Hour - time.Minute)}, 0)
	l.Add(Item{Message: "b", Time: now}, 0)
	if got := l.List(); len(got) != 1 || got[0].Message != "b" {
		t.Fatalf("7 天前的应被清理: %+v", got)
	}
}

func TestMergeInPlace(t *testing.T) {
	now := time.Now()
	l := Open("")
	a := l.Add(Item{Key: "k", Message: "断开 2 次", Time: now}, 5*time.Minute)
	l.Add(Item{Message: "other", Time: now.Add(time.Second)}, 0)
	b := l.Add(Item{Key: "k", Message: "断开 3 次", Time: now.Add(time.Minute)}, 5*time.Minute)
	got := l.List()
	if len(got) != 2 || b.ID != a.ID || got[0].Message != "断开 3 次" {
		t.Fatalf("应原地更新并置顶: %+v", got)
	}
	c := l.Add(Item{Key: "k", Message: "later", Time: now.Add(10 * time.Minute)}, 5*time.Minute)
	if c.ID == a.ID || len(l.List()) != 3 {
		t.Fatal("超出合并窗口应新建一条")
	}
}

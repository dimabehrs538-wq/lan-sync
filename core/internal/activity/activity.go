// Package activity 保存“最近动态”记录到本地文件，并保证有上限：
// 最多 MaxItems 条，且只保留 MaxAge 内的记录（默认 500 条 / 7 天）。
package activity

import (
	"encoding/json"
	"os"
	"sync"
	"time"
)

const (
	DefaultMaxItems = 500
	DefaultMaxAge   = 7 * 24 * time.Hour
)

type Item struct {
	ID      int64     `json:"id"`
	Type    string    `json:"type"`
	Message string    `json:"message"`
	Time    time.Time `json:"time"`
	// Key 非空时，同 Key 且在 MergeWithin 内的记录原地更新（例如同一成员反复断线合并为一条）
	Key   string `json:"key,omitempty"`
	Count int    `json:"count,omitempty"`
}

type Log struct {
	path     string
	MaxItems int
	MaxAge   time.Duration
	Now      func() time.Time

	mu    sync.Mutex
	items []Item // 最新在前
	next  int64
	dirty bool
	timer *time.Timer
}

func Open(path string) *Log {
	l := &Log{path: path, MaxItems: DefaultMaxItems, MaxAge: DefaultMaxAge, Now: time.Now}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, &l.items)
		}
	}
	for _, it := range l.items {
		if it.ID >= l.next {
			l.next = it.ID
		}
	}
	l.mu.Lock()
	l.pruneLocked()
	l.mu.Unlock()
	return l
}

func (l *Log) pruneLocked() {
	cut := l.Now().Add(-l.MaxAge)
	out := l.items[:0]
	for _, it := range l.items {
		if it.Time.After(cut) && len(out) < l.MaxItems {
			out = append(out, it)
		}
	}
	l.items = out
}

// Add 追加一条记录；mergeWithin>0 且存在同 Key、时间在窗口内的记录时原地更新并移到最前。
func (l *Log) Add(it Item, mergeWithin time.Duration) Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	if it.Time.IsZero() {
		it.Time = l.Now()
	}
	if it.Key != "" && mergeWithin > 0 {
		for i, old := range l.items {
			if old.Key == it.Key && it.Time.Sub(old.Time) <= mergeWithin {
				it.ID = old.ID
				l.items = append(l.items[:i], l.items[i+1:]...)
				break
			}
		}
	}
	if it.ID == 0 {
		l.next++
		it.ID = l.next
	}
	l.items = append([]Item{it}, l.items...)
	l.pruneLocked()
	l.scheduleSaveLocked()
	return it
}

func (l *Log) List() []Item {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked()
	return append([]Item{}, l.items...)
}

func (l *Log) Clear() {
	l.mu.Lock()
	l.items = nil
	l.scheduleSaveLocked()
	l.mu.Unlock()
}

// 合并 2 秒内的多次写入，避免频繁写盘
func (l *Log) scheduleSaveLocked() {
	if l.path == "" {
		return
	}
	l.dirty = true
	if l.timer == nil {
		l.timer = time.AfterFunc(2*time.Second, func() { l.Flush() })
	}
}

// Flush 立即写盘（退出时调用）。
func (l *Log) Flush() error {
	l.mu.Lock()
	l.timer = nil
	if !l.dirty || l.path == "" {
		l.mu.Unlock()
		return nil
	}
	b, err := json.Marshal(l.items)
	l.dirty = false
	l.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

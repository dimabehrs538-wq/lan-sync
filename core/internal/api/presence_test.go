package api

import (
	"testing"
	"time"
)

type fakeClock struct {
	now    time.Time
	timers []*fakeTimer
}
type fakeTimer struct {
	at      time.Time
	f       func()
	stopped bool
}

func (c *fakeClock) after(d time.Duration, f func()) func() bool {
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool { s := !t.stopped; t.stopped = true; return s }
}
func (c *fakeClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
	for _, t := range c.timers {
		if !t.stopped && !t.at.After(c.now) {
			t.stopped = true
			t.f()
		}
	}
}

func newTestPresence() (*Presence, *fakeClock, *[]UIEvent) {
	c := &fakeClock{now: time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)}
	evs := &[]UIEvent{}
	p := NewPresence(func(e UIEvent) { *evs = append(*evs, e) })
	p.Now = func() time.Time { return c.now }
	p.After = c.after
	return p, c, evs
}

func messages(evs []UIEvent) []string {
	var m []string
	for _, e := range evs {
		if e.Message != "" {
			m = append(m, e.Message)
		}
	}
	return m
}

func TestDebounceQuickReconnect(t *testing.T) {
	p, c, evs := newTestPresence()
	p.Connected("DEV1234567", "张三")
	*evs = nil
	p.Disconnected("DEV1234567", "")
	c.advance(30 * time.Second)
	p.Connected("DEV1234567", "")
	c.advance(2 * time.Minute)
	if m := messages(*evs); len(m) != 0 {
		t.Fatalf("60 秒内重连不应有动态: %v", m)
	}
}

func TestOfflineAfterDebounce(t *testing.T) {
	p, c, evs := newTestPresence()
	p.Disconnected("DEV1234567", "张三")
	c.advance(59 * time.Second)
	if len(messages(*evs)) != 0 {
		t.Fatal("未满 60 秒不应提示离线")
	}
	c.advance(2 * time.Second)
	if m := messages(*evs); len(m) != 1 || m[0] != "成员 张三 已离线" {
		t.Fatalf("应提示离线: %v", m)
	}
	p.Connected("DEV1234567", "")
	if m := messages(*evs); len(m) != 2 || m[1] != "成员 张三 已上线" {
		t.Fatalf("应提示上线: %v", m)
	}
}

func TestFlapCoalesce(t *testing.T) {
	p, c, evs := newTestPresence()
	id := "DEV1234567"
	for i := 0; i < 4; i++ {
		p.Disconnected(id, "张三")
		c.advance(10 * time.Second)
		p.Connected(id, "")
		c.advance(50 * time.Second)
	}
	var unstable []UIEvent
	for _, e := range *evs {
		switch e.Type {
		case "DeviceUnstable":
			unstable = append(unstable, e)
		}
		if e.Message != "" && e.Type != "DeviceUnstable" {
			t.Fatalf("抖动期间不应逐条提示: %s", e.Message)
		}
	}
	if len(unstable) != 3 || unstable[2].Message != "成员 张三 连接不稳定（5 分钟内断开 4 次）" || unstable[0].Key != unstable[2].Key {
		t.Fatalf("合并记录不正确: %+v", unstable)
	}
}

func TestAdditionalConnectionNotDuplicated(t *testing.T) {
	p, _, evs := newTestPresence()
	p.Connected("D", "张三")
	p.Connected("D", "张三") // v2 额外连接
	if m := messages(*evs); len(m) != 1 {
		t.Fatalf("额外连接不应重复提示上线: %v", m)
	}
}

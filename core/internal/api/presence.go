package api

import (
	"fmt"
	"sort"
	"sync"
	"time"
)

// Presence 处理成员上下线的防抖与断线合并：
//   - 断开后 Debounce（60 秒）内重新连上：不产生“离线/上线”动态；
//   - 同一成员 FlapWindow（5 分钟）内断开 >=2 次：合并为一条“连接不稳定（5 分钟内断开 N 次）”，不再逐条提示上下线；
//   - BannerWindow（10 分钟）内断开 >=BannerThreshold（3）次：Unstable() 返回该成员，前端显示代理/TUN 排查提示。
type Presence struct {
	Debounce        time.Duration
	FlapWindow      time.Duration
	BannerWindow    time.Duration
	BannerThreshold int

	Now   func() time.Time
	After func(d time.Duration, f func()) (stop func() bool)
	Emit  func(UIEvent)

	mu      sync.Mutex
	pending map[string]func() bool // 等待确认离线的计时器
	disc    map[string][]time.Time // 断开时间（只保留 BannerWindow 内）
	names   map[string]string
	up      map[string]bool // 当前已连接（Syncthing v2 每建立一条额外连接都会再发 DeviceConnected）
}

func NewPresence(emit func(UIEvent)) *Presence {
	return &Presence{
		Debounce: 60 * time.Second, FlapWindow: 5 * time.Minute, BannerWindow: 10 * time.Minute, BannerThreshold: 3,
		Now:     time.Now,
		After:   func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop },
		Emit:    emit,
		pending: map[string]func() bool{}, disc: map[string][]time.Time{}, names: map[string]string{}, up: map[string]bool{},
	}
}

func (p *Presence) pruneLocked(id string, now time.Time) []time.Time {
	ts := p.disc[id]
	cut := now.Add(-p.BannerWindow)
	i := 0
	for i < len(ts) && !ts[i].After(cut) {
		i++
	}
	ts = ts[i:]
	if len(ts) == 0 {
		delete(p.disc, id)
	} else {
		p.disc[id] = ts
	}
	return ts
}

// flapCountLocked 返回 5 分钟内断开次数（>=2 视为不稳定）
func (p *Presence) flapCountLocked(id string, now time.Time) int {
	n := 0
	for _, t := range p.pruneLocked(id, now) {
		if t.After(now.Add(-p.FlapWindow)) {
			n++
		}
	}
	return n
}

func (p *Presence) nameLocked(id string) string {
	if n := p.names[id]; n != "" {
		return n
	}
	if len(id) > 7 {
		return id[:7]
	}
	return id
}

func (p *Presence) Disconnected(id, name string) {
	p.mu.Lock()
	now := p.Now()
	if name != "" {
		p.names[id] = name
	}
	delete(p.up, id)
	p.disc[id] = append(p.pruneLocked(id, now), now)
	n := p.flapCountLocked(id, now)
	if stop := p.pending[id]; stop != nil {
		stop()
	}
	p.pending[id] = p.After(p.Debounce, func() { p.confirmOffline(id) })
	var evs []UIEvent
	if n >= 2 {
		evs = append(evs, UIEvent{Type: "DeviceUnstable", Time: now, Key: "unstable:" + id, merge: p.FlapWindow,
			Message: fmt.Sprintf("成员 %s 连接不稳定（5 分钟内断开 %d 次）", p.nameLocked(id), n),
			Data:    map[string]any{"device": id, "count": n}})
	}
	if len(p.disc[id]) == p.BannerThreshold {
		evs = append(evs, UIEvent{Type: "ConnectionUnstable", Time: now, Data: map[string]any{"device": id}})
	}
	p.mu.Unlock()
	for _, e := range evs {
		p.Emit(e)
	}
}

func (p *Presence) confirmOffline(id string) {
	p.mu.Lock()
	if p.pending[id] == nil {
		p.mu.Unlock()
		return
	}
	delete(p.pending, id)
	now := p.Now()
	flap := p.flapCountLocked(id, now) >= 2
	name := p.nameLocked(id)
	p.mu.Unlock()
	e := UIEvent{Type: "DeviceDisconnected", Time: now, Data: map[string]string{"device": id}}
	if !flap { // 不稳定时已有合并记录，不再逐条刷屏
		e.Message = "成员 " + name + " 已离线"
	}
	p.Emit(e)
}

func (p *Presence) Connected(id, name string) {
	p.mu.Lock()
	now := p.Now()
	if name != "" {
		p.names[id] = name
	}
	if p.up[id] {
		p.mu.Unlock()
		return // 同一成员的额外连接，不重复提示
	}
	p.up[id] = true
	if stop := p.pending[id]; stop != nil {
		// 60 秒内重新连上：视为短暂抖动，不提示
		stop()
		delete(p.pending, id)
		p.mu.Unlock()
		p.Emit(UIEvent{Type: "DeviceReconnected", Time: now, Data: map[string]string{"device": id}})
		return
	}
	flap := p.flapCountLocked(id, now) >= 2
	n := p.nameLocked(id)
	p.mu.Unlock()
	e := UIEvent{Type: "DeviceConnected", Time: now, Data: map[string]string{"device": id}}
	if !flap {
		e.Message = "成员 " + n + " 已上线"
	}
	p.Emit(e)
}

type UnstableDevice struct {
	Device string `json:"device"`
	Name   string `json:"name"`
	Count  int    `json:"count"` // 10 分钟内断开次数
}

// Unstable 返回 10 分钟内断开 >=3 次的成员（用于前端提示横幅）。
func (p *Presence) Unstable() []UnstableDevice {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := p.Now()
	out := []UnstableDevice{}
	for id := range p.disc {
		if ts := p.pruneLocked(id, now); len(ts) >= p.BannerThreshold {
			out = append(out, UnstableDevice{Device: id, Name: p.nameLocked(id), Count: len(ts)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Device < out[j].Device })
	return out
}

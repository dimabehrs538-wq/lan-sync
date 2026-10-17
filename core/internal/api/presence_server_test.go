package api

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
)

// 上下线系统通知默认关闭，开启后才 Notify；邀请等其他通知不受影响。断线合并记录在最近动态中原地更新。
func TestPublishPresenceNotifyAndMerge(t *testing.T) {
	store := team.OpenStore(filepath.Join(t.TempDir(), "settings.json"), t.TempDir())
	s := New(stclient.New("http://127.0.0.1:1", "k"), "tok", func() string { return "running" }, store)
	ch := make(chan UIEvent, 16)
	s.subs[ch] = struct{}{}
	now := time.Now()
	s.Publish(UIEvent{Type: "DeviceDisconnected", Message: "成员 A 已离线", Time: now})
	if e := <-ch; e.Notify {
		t.Fatal("默认不应弹上下线系统通知")
	}
	s.Publish(UIEvent{Type: "FolderInvitation", Message: "邀请", Notify: true, Time: now})
	if e := <-ch; !e.Notify {
		t.Fatal("邀请通知不受开关影响")
	}
	store.Update(func(st *team.Settings) { st.PresenceNotify = true })
	s.Publish(UIEvent{Type: "DeviceUnstable", Key: "unstable:A", merge: 5 * time.Minute, Message: "断开 2 次", Time: now})
	e1 := <-ch
	s.Publish(UIEvent{Type: "DeviceUnstable", Key: "unstable:A", merge: 5 * time.Minute, Message: "断开 3 次", Time: now.Add(time.Minute)})
	e2 := <-ch
	if !e1.Notify || e1.ActivityID == 0 || e1.ActivityID != e2.ActivityID {
		t.Fatalf("应开启通知并原地更新: %+v %+v", e1, e2)
	}
	list := s.Activity.List()
	if len(list) != 3 || !strings.Contains(list[0].Message, "3 次") {
		t.Fatalf("最近动态不正确: %+v", list)
	}
}

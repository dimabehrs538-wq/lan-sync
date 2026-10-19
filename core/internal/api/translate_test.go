package api

import (
	"encoding/json"
	"testing"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func TestTranslate(t *testing.T) {
	names := func(id string) string {
		if id == "ABCDEF1" {
			return "张三"
		}
		return id
	}
	ev := stclient.Event{Type: "RemoteChangeDetected", Data: json.RawMessage(`{"path":"设计/接口.md","action":"modified","modifiedBy":"ABCDEF1"}`)}
	u, ok := Translate(ev, names)
	if !ok || u.Message != "张三 修改了 设计/接口.md" || !u.Notify {
		t.Fatalf("%+v", u)
	}
	if _, ok := Translate(stclient.Event{Type: "Ping"}, names); ok {
		t.Fatal("should ignore")
	}
	u, ok = Translate(stclient.Event{Type: "PendingDevicesChanged", Data: json.RawMessage(`{"added":[{"deviceID":"X","name":"李四"}]}`)}, names)
	if !ok || !u.Notify || u.Message == "" {
		t.Fatalf("%+v", u)
	}
}

func TestTranslateRemoteDelete(t *testing.T) {
	e := stclient.Event{Type: "RemoteChangeDetected", Data: []byte(`{"action":"deleted","type":"file","path":"a/b.md","folderID":"f1","modifiedBy":"ABCDEFG"}`)}
	u, ok := Translate(e, func(id string) string {
		if id == "ABCDEFG" {
			return "李四"
		}
		return id
	})
	if !ok || u.Message != "成员 李四 删除了 a/b.md" {
		t.Fatalf("%v %q", ok, u.Message)
	}
	if d := activityData(u); d["folderID"] != "f1" || d["path"] != "a/b.md" || d["action"] != "deleted" {
		t.Fatalf("%v", d)
	}
	if _, ok := Translate(stclient.Event{Type: "RemoteChangeDetected", Data: []byte(`{"action":"modified","path":".teamsync.json"}`)}, func(s string) string { return s }); ok {
		t.Fatal("模式文件不应出现在动态中")
	}
}

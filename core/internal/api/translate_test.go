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

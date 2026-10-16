package api

import (
	"encoding/json"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

var actionCN = map[string]string{"added": "新增了", "modified": "修改了", "deleted": "删除了", "update": "更新了", "delete": "删除了", "metadata": "更新了"}

// Translate 把 Syncthing 事件翻译为前端事件；返回 false 表示忽略。
func Translate(e stclient.Event, name func(string) string) (UIEvent, bool) {
	var d map[string]any
	_ = json.Unmarshal(e.Data, &d)
	str := func(k string) string { v, _ := d[k].(string); return v }
	u := UIEvent{Type: e.Type, Time: e.Time}
	switch e.Type {
	case "DeviceConnected":
		u.Message = "成员 " + name(str("id")) + " 已上线"
		u.Data = map[string]string{"device": str("id")}
	case "DeviceDisconnected":
		u.Message = "成员 " + name(str("id")) + " 已离线"
		u.Data = map[string]string{"device": str("id")}
	case "PendingDevicesChanged":
		added, _ := d["added"].([]any)
		if len(added) == 0 {
			return u, false
		}
		first, _ := added[0].(map[string]any)
		n, _ := first["name"].(string)
		id, _ := first["deviceID"].(string)
		if n == "" {
			n = name(id)
		}
		u.Message, u.Notify, u.Data = "设备 "+n+" 请求加入团队，请在“成员”页确认", true, first
	case "PendingFoldersChanged":
		u.Data = d // 由事件桥转换成邀请通知
	case "RemoteChangeDetected":
		act := actionCN[str("action")]
		if act == "" {
			act = "更新了"
		}
		u.Message = name(str("modifiedBy")) + " " + act + " " + str("path")
		u.Notify = true
		u.Data = d
	case "LocalChangeDetected":
		u.Data = d
	case "FolderSummary", "StateChanged", "FolderCompletion":
		u.Data = d
	case "FolderErrors":
		u.Message, u.Notify, u.Data = "文件夹同步出错，请查看详情", true, d
	case "ConfigSaved":
	default:
		return u, false
	}
	return u, true
}

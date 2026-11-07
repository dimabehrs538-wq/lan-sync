package mobilecore

import (
	"testing"

	"github.com/syncthing/syncthing/lib/config"
)

// 首次启动前应用的选项必须与桌面内核写入的纯内网默认值一致
func TestApplyLANOptions(t *testing.T) {
	o := config.New([32]byte{}).Options
	if !o.GlobalAnnEnabled || !o.RelaysEnabled || !o.NATEnabled {
		t.Fatal("syncthing 默认值变化，测试前提不成立")
	}
	applyLANOptions(&o)
	if o.GlobalAnnEnabled || o.RelaysEnabled || o.NATEnabled || !o.LocalAnnEnabled || o.URAccepted != -1 || o.CREnabled || o.AutoUpgradeIntervalH != 0 || o.StartBrowser || o.ReconnectIntervalS != 10 || len(o.AlwaysLocalNets) != 3 {
		t.Fatalf("内网默认值未生效: %+v", o)
	}
	if o.ListenAddresses()[0] == "" {
		t.Fatal("其他选项应保留")
	}
}

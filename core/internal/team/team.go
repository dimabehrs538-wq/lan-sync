// Package team 提供"团队文件夹/成员"的简化模型，封装为 Syncthing 配置操作。
package team

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

// LANOptions 纯内网默认：关闭全局发现、中继、NAT 穿透、使用统计；开启局域网发现。
var LANOptions = map[string]any{
	"globalAnnounceEnabled": false,
	"relaysEnabled":         false,
	"natEnabled":            false,
	"localAnnounceEnabled":  true,
	"urAccepted":            -1,
	"crashReportingEnabled": false,
	"autoUpgradeIntervalH":  0,
	"startBrowser":          false,
}

// DefaultIgnores 默认忽略规则（Office 临时文件、系统文件等）。
var DefaultIgnores = []string{
	"(?d)~$*", "(?d)*.tmp", "(?d).~lock.*", "(?d).DS_Store", "(?d)Thumbs.db", "(?d)desktop.ini",
}

const idAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"

// NewFolderID 生成 "xxxxx-xxxxx" 形式的文件夹 ID。
func NewFolderID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	out := make([]byte, 0, 11)
	for i, v := range b {
		if i == 5 {
			out = append(out, '-')
		}
		out = append(out, idAlphabet[int(v)%len(idAlphabet)])
	}
	return string(out)
}

// NewTeamFolder 构造默认开启文件监视 + 简单版本控制（保留 10 份、30 天）的文件夹配置。
func NewTeamFolder(label, path string, deviceIDs []string) stclient.Folder {
	f := stclient.Folder{
		ID:               NewFolderID(),
		Label:            label,
		Path:             path,
		Type:             "sendreceive",
		RescanIntervalS:  3600,
		FSWatcherEnabled: true,
		FSWatcherDelayS:  2,
		Versioning: stclient.Versioning{
			Type:             "simple",
			Params:           map[string]string{"keep": "10", "cleanoutDays": "30"},
			CleanupIntervalS: 3600,
		},
	}
	for _, d := range uniq(deviceIDs) {
		f.Devices = append(f.Devices, stclient.FolderDevice{DeviceID: d})
	}
	return f
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// WriteIgnores 若文件夹中没有 .stignore，则写入默认忽略规则。
func WriteIgnores(dir string) error {
	p := filepath.Join(dir, ".stignore")
	if _, err := os.Stat(p); err == nil {
		return nil
	}
	return os.WriteFile(p, []byte(strings.Join(DefaultIgnores, "\n")+"\n"), 0o644)
}

type Service struct{ C *stclient.Client }

func (s *Service) ApplyLANDefaults(ctx context.Context) error {
	return s.C.PatchOptions(ctx, LANOptions)
}

func (s *Service) MyID(ctx context.Context) (string, error) {
	st, err := s.C.Status(ctx)
	return st.MyID, err
}

// CreateFolder 创建团队文件夹，并共享给所有已添加的成员。
func (s *Service) CreateFolder(ctx context.Context, label, path string) (stclient.Folder, error) {
	if label == "" || path == "" {
		return stclient.Folder{}, errors.New("名称和路径不能为空")
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return stclient.Folder{}, err
	}
	_ = WriteIgnores(path)
	my, err := s.MyID(ctx)
	if err != nil {
		return stclient.Folder{}, err
	}
	ids := []string{my}
	devs, err := s.C.Devices(ctx)
	if err != nil {
		return stclient.Folder{}, err
	}
	for _, d := range devs {
		ids = append(ids, d.DeviceID)
	}
	f := NewTeamFolder(label, path, ids)
	return f, s.C.PutFolder(ctx, f)
}

// AddMember 添加成员设备（地址 dynamic，靠局域网发现），并把所有团队文件夹共享给它。
// 对团队成员开启 autoAcceptFolders，对方共享的文件夹会自动接收。
func (s *Service) AddMember(ctx context.Context, deviceID, name string) error {
	deviceID = strings.TrimSpace(deviceID)
	if deviceID == "" {
		return errors.New("设备 ID 不能为空")
	}
	if err := s.C.PutDevice(ctx, stclient.Device{
		DeviceID: deviceID, Name: name, Addresses: []string{"dynamic"}, AutoAcceptFolders: true,
	}); err != nil {
		return err
	}
	folders, err := s.C.Folders(ctx)
	if err != nil {
		return err
	}
	for _, f := range folders {
		if hasDevice(f, deviceID) {
			continue
		}
		f.Devices = append(f.Devices, stclient.FolderDevice{DeviceID: deviceID})
		if err := s.C.PatchFolder(ctx, f.ID, map[string]any{"devices": f.Devices}); err != nil {
			return err
		}
	}
	return nil
}

func hasDevice(f stclient.Folder, id string) bool {
	for _, d := range f.Devices {
		if d.DeviceID == id {
			return true
		}
	}
	return false
}

// AcceptPending 接受请求连接的设备。
func (s *Service) AcceptPending(ctx context.Context, deviceID string) error {
	p, err := s.C.PendingDevices(ctx)
	if err != nil {
		return err
	}
	name := ""
	if pd, ok := p[deviceID]; ok {
		name = pd.Name
	}
	return s.AddMember(ctx, deviceID, name)
}

type Nearby struct {
	DeviceID  string   `json:"deviceID"`
	Addresses []string `json:"addresses"`
	Name      string   `json:"name,omitempty"`
	Pending   bool     `json:"pending"`
}

// NearbyDevices 列出局域网发现到、但尚未添加的设备（合并待确认设备）。
func (s *Service) NearbyDevices(ctx context.Context) ([]Nearby, error) {
	my, err := s.MyID(ctx)
	if err != nil {
		return nil, err
	}
	devs, err := s.C.Devices(ctx)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{my: true}
	for _, d := range devs {
		known[d.DeviceID] = true
	}
	res := map[string]*Nearby{}
	if disc, err := s.C.Discovery(ctx); err == nil {
		for id, v := range disc {
			if !known[id] {
				res[id] = &Nearby{DeviceID: id, Addresses: v.Addresses}
			}
		}
	}
	if pend, err := s.C.PendingDevices(ctx); err == nil {
		for id, p := range pend {
			if known[id] {
				continue
			}
			n := res[id]
			if n == nil {
				n = &Nearby{DeviceID: id}
				res[id] = n
			}
			n.Pending, n.Name = true, p.Name
			if p.Address != "" && len(n.Addresses) == 0 {
				n.Addresses = []string{p.Address}
			}
		}
	}
	out := make([]Nearby, 0, len(res))
	for _, n := range res {
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out, nil
}

// FolderDefaults 返回 Syncthing "新文件夹默认值"：自动接收的团队文件夹放在 baseDir/<名称> 下，
// 并同样开启文件监视和简单版本控制。
func FolderDefaults(baseDir string) map[string]any {
	f := NewTeamFolder("", baseDir, nil)
	return map[string]any{
		"path":             baseDir,
		"fsWatcherEnabled": true,
		"fsWatcherDelayS":  f.FSWatcherDelayS,
		"rescanIntervalS":  f.RescanIntervalS,
		"versioning":       f.Versioning,
	}
}

// ApplyFolderDefaults 设置新文件夹默认值。
func (s *Service) ApplyFolderDefaults(ctx context.Context, baseDir string) error {
	_ = os.MkdirAll(baseDir, 0o755)
	return s.C.Do(ctx, "PATCH", "/rest/config/defaults/folder", nil, FolderDefaults(baseDir), nil)
}

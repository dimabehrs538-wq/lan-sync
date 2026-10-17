package team

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

// Settings 是内核自身的持久化设置（home/settings.json）。
type Settings struct {
	DefaultBaseDir string          `json:"defaultBaseDir"`
	Ignored        []IgnoredInvite `json:"ignored"`
	// Remarks 记录本机用户手动设置过备注名的成员（优先于对方自己设置的名称）。
	Remarks map[string]bool `json:"remarks,omitempty"`
	// Announced 记录成员上线时自己通告的名称（"我的名称"）。
	Announced       map[string]string `json:"announced,omitempty"`
	RemarksMigrated bool              `json:"remarksMigrated,omitempty"`
	// PresenceNotify 成员上下线是否弹系统通知（默认关闭；邀请、冲突等通知不受影响）
	PresenceNotify bool `json:"presenceNotify,omitempty"`
	// OverlapPaused 记录因与其他文件夹位置重叠而被自动暂停的文件夹
	OverlapPaused map[string]bool `json:"overlapPaused,omitempty"`
	// AddedAt 记录文件夹在本机添加（新建/接受邀请）的时间，用于判断重叠时哪个是后添加的
	AddedAt map[string]time.Time `json:"addedAt,omitempty"`
}

// IgnoredInvite 记录被忽略（或本机已移除）的文件夹邀请（文件夹 + 邀请人）。
// 对方"重新邀请"时会先撤回再发出邀请，内核收到撤回事件（PendingFoldersChanged.removed）
// 后清除这条记录，新的邀请就会重新出现；仅重连不会清除。
type IgnoredInvite struct {
	FolderID  string `json:"folderID"`
	DeviceID  string `json:"deviceID"`
	OfferTime string `json:"offerTime"`
}

type Store struct {
	path string
	mu   sync.Mutex
	s    Settings
}

func OpenStore(path, defaultBase string) *Store {
	st := &Store{path: path}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &st.s)
	}
	if st.s.DefaultBaseDir == "" {
		st.s.DefaultBaseDir = defaultBase
	}
	return st
}

func (st *Store) Get() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := st.s
	out.Ignored = append([]IgnoredInvite(nil), st.s.Ignored...)
	return out
}

func (st *Store) Update(fn func(*Settings)) error {
	st.mu.Lock()
	defer st.mu.Unlock()
	fn(&st.s)
	b, _ := json.MarshalIndent(st.s, "", "  ")
	if err := os.MkdirAll(filepath.Dir(st.path), 0o700); err != nil {
		return err
	}
	return os.WriteFile(st.path, b, 0o600)
}

func (st *Store) IsIgnored(folderID, deviceID string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, i := range st.s.Ignored {
		if i.FolderID == folderID && i.DeviceID == deviceID {
			return true
		}
	}
	return false
}

func (st *Store) Ignore(folderID, deviceID, offerTime string) error {
	return st.Update(func(s *Settings) {
		out := s.Ignored[:0]
		for _, i := range s.Ignored {
			if !(i.FolderID == folderID && i.DeviceID == deviceID) {
				out = append(out, i)
			}
		}
		s.Ignored = append(out, IgnoredInvite{folderID, deviceID, offerTime})
	})
}

// UnignorePair 清除某个邀请人对某文件夹的忽略记录（对方撤回邀请时调用）；返回是否有变化。
func (st *Store) UnignorePair(folderID, deviceID string) bool {
	if !st.IsIgnored(folderID, deviceID) {
		return false
	}
	_ = st.Update(func(s *Settings) {
		out := s.Ignored[:0]
		for _, i := range s.Ignored {
			if !(i.FolderID == folderID && i.DeviceID == deviceID) {
				out = append(out, i)
			}
		}
		s.Ignored = out
	})
	return true
}

func (st *Store) Unignore(folderID string) error {
	return st.Update(func(s *Settings) {
		out := s.Ignored[:0]
		for _, i := range s.Ignored {
			if i.FolderID != folderID {
				out = append(out, i)
			}
		}
		s.Ignored = out
	})
}

// Invitation 是一条待处理的文件夹共享邀请。
type Invitation struct {
	FolderID    string    `json:"folderID"`
	Label       string    `json:"label"`
	DeviceID    string    `json:"deviceID"`
	DeviceName  string    `json:"deviceName"`
	Time        time.Time `json:"time"`
	OfferTime   string    `json:"offerTime"`
	SuggestPath string    `json:"suggestPath"`
}

// FilterInvitations 把 Syncthing 的待处理文件夹转成邀请列表，去掉已忽略的和本机已有的文件夹。
func FilterInvitations(pending map[string]stclient.PendingFolder, have map[string]bool, names map[string]string, st *Store, existing []stclient.Folder) []Invitation {
	base := st.Get().DefaultBaseDir
	out := []Invitation{}
	for fid, p := range pending {
		if have[fid] {
			continue
		}
		for dev, o := range p.OfferedBy {
			if st.IsIgnored(fid, dev) {
				continue
			}
			label := o.Label
			if label == "" {
				label = fid
			}
			t, _ := time.Parse(time.RFC3339Nano, o.Time)
			out = append(out, Invitation{FolderID: fid, Label: label, DeviceID: dev, DeviceName: names[dev], Time: t, OfferTime: o.Time,
				SuggestPath: SuggestPath(base, label, existing)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// Invitations 查询当前邀请。
func (s *Service) Invitations(ctx context.Context, st *Store) ([]Invitation, error) {
	pending, err := s.C.PendingFolders(ctx)
	if err != nil {
		return nil, err
	}
	folders, err := s.C.Folders(ctx)
	if err != nil {
		return nil, err
	}
	have := map[string]bool{}
	for _, f := range folders {
		have[f.ID] = true
	}
	names := map[string]string{}
	if devs, err := s.C.Devices(ctx); err == nil {
		for _, d := range devs {
			names[d.DeviceID] = d.Name
		}
	}
	return FilterInvitations(pending, have, names, st, folders), nil
}

// ReconnectPause 是移除文件夹后暂停连接的时长（测试中可调小）。
var ReconnectPause = time.Second

// RemoveResult 是移除文件夹的结果。
type RemoveResult struct {
	Path         string `json:"path"`
	FilesDeleted bool   `json:"filesDeleted"`
	Trashed      bool   `json:"trashed"` // true=已移到回收站
}

// RemoveFolder 仅在本机移除团队文件夹：从 Syncthing 配置删除（其他成员不受影响），
// 并把当前所有共享者记入忽略，避免立刻又作为邀请/自动文件夹出现；对方重新邀请时会再次出现。
// deleteFiles 为 true 时把本地文件移到回收站（失败且 allowPermanent 时永久删除）。
func (s *Service) RemoveFolder(ctx context.Context, st *Store, folderID string, deleteFiles, allowPermanent bool, trashFn func(string, bool) (bool, error)) (RemoveResult, error) {
	f, err := s.C.Folder(ctx, folderID)
	if err != nil {
		return RemoveResult{}, err
	}
	my, _ := s.MyID(ctx)
	if err := s.C.Do(ctx, "DELETE", "/rest/config/folders/"+folderID, nil, nil, nil); err != nil {
		return RemoveResult{}, err
	}
	for _, d := range f.Devices {
		if d.DeviceID != my {
			_ = st.Ignore(folderID, d.DeviceID, "removed")
		}
	}
	// 短暂暂停/恢复这些成员的连接，让对方重新发送集群配置：本机会记下一条（已忽略的）待处理邀请，
	// 这样对方之后"重新邀请"时先撤回再邀请，内核能收到撤回事件并重新显示邀请。
	for _, d := range f.Devices {
		if d.DeviceID != my {
			_ = s.C.PatchDevice(ctx, d.DeviceID, map[string]any{"paused": true})
		}
	}
	time.Sleep(ReconnectPause)
	for _, d := range f.Devices {
		if d.DeviceID != my {
			_ = s.C.PatchDevice(ctx, d.DeviceID, map[string]any{"paused": false})
		}
	}
	res := RemoveResult{Path: f.Path}
	if deleteFiles {
		trashed, err := trashFn(f.Path, allowPermanent)
		if err != nil {
			return res, err
		}
		res.FilesDeleted, res.Trashed = true, trashed
	}
	return res, nil
}

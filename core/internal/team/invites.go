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
}

// IgnoredInvite 记录被忽略的某一次邀请（文件夹 + 邀请人 + 邀请时间）。
// 对方重新邀请会产生新的邀请时间，因此会重新出现。
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

func (st *Store) IsIgnored(folderID, deviceID, offerTime string) bool {
	st.mu.Lock()
	defer st.mu.Unlock()
	for _, i := range st.s.Ignored {
		if i.FolderID == folderID && i.DeviceID == deviceID && i.OfferTime == offerTime {
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
func FilterInvitations(pending map[string]stclient.PendingFolder, have map[string]bool, names map[string]string, st *Store) []Invitation {
	base := st.Get().DefaultBaseDir
	out := []Invitation{}
	for fid, p := range pending {
		if have[fid] {
			continue
		}
		for dev, o := range p.OfferedBy {
			if st.IsIgnored(fid, dev, o.Time) {
				continue
			}
			label := o.Label
			if label == "" {
				label = fid
			}
			t, _ := time.Parse(time.RFC3339Nano, o.Time)
			out = append(out, Invitation{FolderID: fid, Label: label, DeviceID: dev, DeviceName: names[dev], Time: t, OfferTime: o.Time,
				SuggestPath: filepath.Join(base, SafeName(label))})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out
}

// SafeName 去掉 Windows 文件名非法字符。
func SafeName(s string) string {
	r := []rune{}
	for _, c := range s {
		switch c {
		case '<', '>', ':', '"', '/', '\\', '|', '?', '*':
			c = '_'
		}
		if c >= 32 {
			r = append(r, c)
		}
	}
	if len(r) == 0 {
		return "folder"
	}
	return string(r)
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
	return FilterInvitations(pending, have, names, st), nil
}

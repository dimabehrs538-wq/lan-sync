package team

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

// 只读共享（v0.2.9）
//
// 共享者一侧为“仅发送”(sendonly)，成员一侧为“仅接收”(receiveonly)：成员本机的改动不会同步出去。
// 模式通过两种方式传递给成员：
//  1. 邀请时：文件夹名称末尾带 ReadOnlyMarker（Syncthing 邀请只携带名称），成员接受时直接设为仅接收；
//  2. 之后切换：共享者在文件夹根目录写入 ModeFile（随文件夹同步），成员内核读到后自动切换类型。

const (
	ReadOnlyMarker = " [只读]"
	ModeFile       = ".teamsync.json"
	ModeCollab     = "collab"
	ModeReadOnly   = "readonly"
)

// ParseLabel 去掉名称中的只读标记，返回显示名称与是否只读。
func ParseLabel(label string) (string, bool) {
	if strings.HasSuffix(label, ReadOnlyMarker) {
		return strings.TrimSuffix(label, ReadOnlyMarker), true
	}
	return label, false
}

// MakeLabel 生成共享者一侧的文件夹名称（只读时带标记，供邀请传递模式）。
func MakeLabel(name string, readOnly bool) string {
	name, _ = ParseLabel(name)
	if readOnly {
		return name + ReadOnlyMarker
	}
	return name
}

// ShareMode 是写在文件夹中的共享模式。
type ShareMode struct {
	Mode      string    `json:"mode"`  // collab | readonly
	Owner     string    `json:"owner"` // 共享者设备 ID
	UpdatedAt time.Time `json:"updatedAt"`
}

func ReadMode(dir string) (ShareMode, bool) {
	var m ShareMode
	b, err := os.ReadFile(filepath.Join(dir, ModeFile))
	if err != nil || json.Unmarshal(b, &m) != nil || (m.Mode != ModeCollab && m.Mode != ModeReadOnly) {
		return ShareMode{}, false
	}
	return m, true
}

func WriteMode(dir string, m ShareMode) error {
	b, _ := json.MarshalIndent(m, "", "  ")
	p := filepath.Join(dir, ModeFile)
	unhideMarker(p) // 切换模式时覆盖已隐藏的旧文件
	if err := os.WriteFile(p, b, 0o644); err != nil {
		return err
	}
	hideMarker(p)
	return nil
}

// FolderMode 返回文件夹的模式与本机角色（owner=共享者 / member=成员 / ""=协作，无共享者）。
func FolderMode(f stclient.Folder, my string) (mode, role string) {
	switch f.Type {
	case "sendonly":
		return ModeReadOnly, "owner"
	case "receiveonly":
		return ModeReadOnly, "member"
	}
	if m, ok := ReadMode(f.Path); ok && m.Mode == ModeReadOnly && m.Owner != my {
		return ModeReadOnly, "member" // 已收到只读设置，等待切换
	}
	return ModeCollab, ""
}

var ErrNotOwner = errors.New("只有共享者可以修改这个文件夹的共享模式")

// SetMode 由共享者切换模式：只读=仅发送 + 名称标记 + 模式文件；协作=收发 + 模式文件。
func (s *Service) SetMode(ctx context.Context, id, mode string) error {
	if mode != ModeCollab && mode != ModeReadOnly {
		return errors.New("未知的共享模式")
	}
	f, err := s.C.Folder(ctx, id)
	if err != nil {
		return err
	}
	my, err := s.MyID(ctx)
	if err != nil {
		return err
	}
	if f.Type == "receiveonly" {
		return ErrNotOwner
	}
	if m, ok := ReadMode(f.Path); ok && m.Owner != "" && m.Owner != my {
		return ErrNotOwner
	}
	typ := "sendreceive"
	if mode == ModeReadOnly {
		typ = "sendonly"
	}
	if err := WriteMode(f.Path, ShareMode{Mode: mode, Owner: my, UpdatedAt: time.Now().UTC()}); err != nil {
		return err
	}
	name, _ := ParseLabel(f.Label)
	if err := s.C.PatchFolder(ctx, id, map[string]any{"type": typ, "label": MakeLabel(name, mode == ModeReadOnly)}); err != nil {
		return err
	}
	_ = s.C.Scan(ctx, id, "")
	return nil
}

// ModeChange 描述一次由模式文件触发的本机切换。
type ModeChange struct {
	FolderID string
	Label    string
	ReadOnly bool
}

// SyncModes 让成员一侧跟随共享者写入的模式文件：只读 -> 仅接收；协作 -> 收发。
func (s *Service) SyncModes(ctx context.Context) ([]ModeChange, error) {
	my, err := s.MyID(ctx)
	if err != nil {
		return nil, err
	}
	fs, err := s.C.Folders(ctx)
	if err != nil {
		return nil, err
	}
	var out []ModeChange
	for _, f := range fs {
		m, ok := ReadMode(f.Path)
		if !ok || m.Owner == "" || m.Owner == my || f.Type == "sendonly" {
			continue
		}
		want := "sendreceive"
		if m.Mode == ModeReadOnly {
			want = "receiveonly"
		}
		if f.Type == want || (f.Type != "sendreceive" && f.Type != "receiveonly") {
			continue
		}
		if err := s.C.PatchFolder(ctx, f.ID, map[string]any{"type": want}); err != nil {
			continue
		}
		name, _ := ParseLabel(f.Label)
		out = append(out, ModeChange{FolderID: f.ID, Label: name, ReadOnly: want == "receiveonly"})
	}
	return out, nil
}

// SetDeleteProtection 开关删除保护（简单版本控制，按保留规则；被删除或覆盖的文件进入 .stversions）。
func (s *Service) SetDeleteProtection(ctx context.Context, id string, on bool, r Retention) error {
	v := stclient.Versioning{Type: "", Params: map[string]string{}}
	if on {
		v = VersioningFor(r)
	}
	return s.C.PatchFolder(ctx, id, map[string]any{"versioning": v})
}

func DefaultVersioning() stclient.Versioning {
	return VersioningFor(DefaultRetention)
}

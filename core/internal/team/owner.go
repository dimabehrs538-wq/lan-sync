package team

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

// 共享者（v0.3.1）
//
// 每个文件夹的共享者（只有共享者能切换协作 / 只读）记录在本机设置 Settings.Owners 中，
// 一旦确定就不再改变，也绝不从随文件夹同步的 .teamsync.json 推导：
//   - 新建文件夹：共享者 = 本机；
//   - 接受邀请：共享者 = 发出这条邀请（你选择接受）的设备；
//   - 旧版本升级的文件夹见 decideOwner（迁移规则）；无法确定时记为“未知”，此时谁都不能切换模式，
//     直到用户在本机手动指定共享者（只影响本机，其他成员仍按各自记录判断）。
//
// 成员一侧只在以下条件全部满足时才跟随 .teamsync.json：文件中的 owner 等于本机记录的共享者，
// 且引擎数据库中该文件的全局版本和本机版本都是由共享者设备最后修改的（modifiedBy），
// 本机磁盘上的文件也与数据库记录一致（没有尚未扫描的本机改动）。

var ErrOwnerFixed = errors.New("这个文件夹的共享者已经确定，不能修改")

// Owner 返回本机记录的共享者；recorded=false 表示还没有记录（尚未迁移），owner="" 且 recorded=true 表示未知。
func (st *Store) Owner(id string) (owner string, recorded bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	owner, recorded = st.s.Owners[id]
	return
}

func (st *Store) setOwner(id, dev string, allow func(cur string, recorded bool) error) error {
	var err error
	uerr := st.Update(func(s *Settings) {
		cur, ok := s.Owners[id]
		if err = allow(cur, ok); err != nil {
			return
		}
		if s.Owners == nil {
			s.Owners = map[string]string{}
		}
		s.Owners[id] = dev
	})
	if err != nil {
		return err
	}
	return uerr
}

// RecordNewFolderOwner 在本机新加入文件夹（新建 / 接受邀请）时记录共享者。
func (st *Store) RecordNewFolderOwner(id, dev string) error {
	return st.setOwner(id, dev, func(string, bool) error { return nil })
}

// RecordOwnerIfMissing 迁移用：仅在还没有记录时写入（dev 可为空，表示未知）。
func (st *Store) RecordOwnerIfMissing(id, dev string) (bool, error) {
	wrote := false
	err := st.setOwner(id, dev, func(_ string, ok bool) error {
		if ok {
			return ErrOwnerFixed
		}
		wrote = true
		return nil
	})
	if errors.Is(err, ErrOwnerFixed) {
		return false, nil
	}
	return wrote, err
}

// AssignOwner 由用户在本机手动指定共享者：只允许在共享者未知时进行。
func (st *Store) AssignOwner(id, dev string) error {
	if dev == "" {
		return errors.New("请选择共享者")
	}
	return st.setOwner(id, dev, func(cur string, _ bool) error {
		if cur != "" {
			return ErrOwnerFixed
		}
		return nil
	})
}

// ForgetOwner 本机移除文件夹时清除记录（之后重新接受邀请时重新记录）。
func (st *Store) ForgetOwner(id string) error {
	return st.Update(func(s *Settings) { delete(s.Owners, id) })
}

// decideOwner 是旧文件夹的迁移规则（按顺序，取第一个成立的）：
//  1. 本机为“仅发送”（只读共享者）→ 本机；
//  2. 协作文件夹且本机有 TeamSync 新建时写入的 .stignore（接受邀请时不会写入，且 .stignore 不会同步）→ 本机；
//  3. 存在可信的 .teamsync.json（全局与本机版本都由文件中的 owner 设备修改，且该设备是文件夹成员）
//     且 owner 不是本机 → 该设备（本机自称共享者的文件不采信）；
//  4. 文件夹中除本机外只有一台设备（接受邀请时只加入邀请人）→ 该设备；
//  5. 否则未知。
func decideOwner(typ, my string, devices []string, createdHere bool, trustedFileOwner string) string {
	others := []string{}
	isMember := map[string]bool{}
	for _, d := range devices {
		isMember[d] = true
		if d != my {
			others = append(others, d)
		}
	}
	switch {
	case typ == "sendonly":
		return my
	case typ == "sendreceive" && createdHere:
		return my
	case trustedFileOwner != "" && trustedFileOwner != my && isMember[trustedFileOwner]:
		return trustedFileOwner
	case len(others) == 1:
		return others[0]
	}
	return ""
}

func folderDevices(f stclient.Folder) []string {
	out := make([]string, 0, len(f.Devices))
	for _, d := range f.Devices {
		out = append(out, d.DeviceID)
	}
	return out
}

// TrustedMode 读取 .teamsync.json，仅当它确实来自 owner 设备时返回 ok=true；否则 reason 说明原因（无文件时为空）。
func (s *Service) TrustedMode(ctx context.Context, f stclient.Folder, owner string) (m ShareMode, ok bool, reason string) {
	m, exists := ReadMode(f.Path)
	if !exists {
		return ShareMode{}, false, ""
	}
	if owner == "" {
		return m, false, "本机没有记录共享者"
	}
	if m.Owner != owner {
		return m, false, fmt.Sprintf("文件中的共享者 %s 与本机记录的共享者 %s 不一致", stclient.ShortID(m.Owner), stclient.ShortID(owner))
	}
	db, err := s.C.DBFile(ctx, f.ID, ModeFile)
	if err != nil {
		return m, false, "无法查询文件记录：" + err.Error()
	}
	short := stclient.ShortID(owner)
	if db.Global.ModifiedBy != short || db.Local.ModifiedBy != short {
		return m, false, fmt.Sprintf("文件最后由 %s / %s 修改，不是共享者 %s", db.Global.ModifiedBy, db.Local.ModifiedBy, short)
	}
	if db.Local.Deleted || db.Local.Invalid || db.Global.Deleted {
		return m, false, "文件记录已删除或无效"
	}
	fi, err := os.Stat(filepath.Join(f.Path, ModeFile))
	if err != nil || fi.Size() != db.Local.Size || absDur(fi.ModTime().Sub(db.Local.Modified)) > 2*time.Second {
		return m, false, "文件在本机被改动（与引擎记录不一致）"
	}
	return m, true, ""
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

// MigrateOwners 为还没有共享者记录的文件夹（旧版本升级）按 decideOwner 记录共享者，返回日志。
func (s *Service) MigrateOwners(ctx context.Context, st *Store) []string {
	my, err := s.MyID(ctx)
	if err != nil {
		return nil
	}
	fs, err := s.C.Folders(ctx)
	if err != nil {
		return nil
	}
	var logs []string
	for _, f := range fs {
		if _, ok := st.Owner(f.ID); ok {
			continue
		}
		fileOwner := ""
		if m, exists := ReadMode(f.Path); exists && m.Owner != "" {
			if _, ok, _ := s.TrustedMode(ctx, f, m.Owner); ok {
				fileOwner = m.Owner
			}
		}
		_, err := os.Stat(filepath.Join(f.Path, ".stignore"))
		owner := decideOwner(f.Type, my, folderDevices(f), err == nil, fileOwner)
		if wrote, _ := st.RecordOwnerIfMissing(f.ID, owner); wrote {
			who := "未知"
			if owner == my {
				who = "本机"
			} else if owner != "" {
				who = stclient.ShortID(owner)
			}
			logs = append(logs, fmt.Sprintf("迁移：文件夹 %s 的共享者 = %s", f.ID, who))
		}
	}
	return logs
}

package team

import (
	"context"
	"time"
)

// HasRemark 表示本机用户是否手动给该成员设置过备注名。
func (st *Store) HasRemark(id string) bool {
	if st == nil {
		return false
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Remarks[id]
}

// SetRemark 记录/清除手动备注标记。
func (st *Store) SetRemark(id string, on bool) error {
	if st == nil {
		return nil
	}
	return st.Update(func(s *Settings) {
		if s.Remarks == nil {
			s.Remarks = map[string]bool{}
		}
		if on {
			s.Remarks[id] = true
		} else {
			delete(s.Remarks, id)
		}
	})
}

// Announced 返回成员自己通告的名称。
func (st *Store) Announced(id string) string {
	if st == nil {
		return ""
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.s.Announced[id]
}

func (st *Store) setAnnounced(id, name string) error {
	if st.Announced(id) == name {
		return nil
	}
	return st.Update(func(s *Settings) {
		if s.Announced == nil {
			s.Announced = map[string]string{}
		}
		s.Announced[id] = name
	})
}

// MigrateRemarks 从旧版本升级时，把已有成员的名称视为手动备注（不被对方的名称覆盖）。
func (s *Service) MigrateRemarks(ctx context.Context, st *Store) error {
	if st == nil || st.Get().RemarksMigrated {
		return nil
	}
	devs, err := s.C.Devices(ctx)
	if err != nil {
		return err
	}
	my, _ := s.MyID(ctx)
	return st.Update(func(x *Settings) {
		if x.Remarks == nil {
			x.Remarks = map[string]bool{}
		}
		for _, d := range devs {
			if d.DeviceID != my && d.Name != "" {
				x.Remarks[d.DeviceID] = true
			}
		}
		x.RemarksMigrated = true
	})
}

// ApplyAnnounced 处理成员上线时通告的名称：记录下来；本机没有手动备注时用它作为显示名称。
func (s *Service) ApplyAnnounced(ctx context.Context, st *Store, id, name string) (bool, error) {
	if name == "" || st == nil {
		return false, nil
	}
	if err := st.setAnnounced(id, name); err != nil {
		return false, err
	}
	if st.HasRemark(id) {
		return false, nil
	}
	devs, err := s.C.Devices(ctx)
	if err != nil {
		return false, err
	}
	for _, d := range devs {
		if d.DeviceID == id && d.Name != name {
			return true, s.C.PatchDevice(ctx, id, map[string]any{"name": name})
		}
	}
	return false, nil
}

// ClearRemark 清除手动备注，改用对方自己设置的名称。
func (s *Service) ClearRemark(ctx context.Context, st *Store, id string) (string, error) {
	if err := st.SetRemark(id, false); err != nil {
		return "", err
	}
	n := st.Announced(id)
	if n != "" {
		return n, s.C.PatchDevice(ctx, id, map[string]any{"name": n})
	}
	return n, nil
}

// SetMyName 修改本机名称（"我的名称"），并让所有成员重连一次以便立即收到新名称。
func (s *Service) SetMyName(ctx context.Context, name string) (string, error) {
	name, err := NormalizeName(name)
	if err != nil {
		return "", err
	}
	my, err := s.MyID(ctx)
	if err != nil {
		return "", err
	}
	if err := s.C.PatchDevice(ctx, my, map[string]any{"name": name}); err != nil {
		return "", err
	}
	devs, _ := s.C.Devices(ctx)
	var peers []string
	for _, d := range devs {
		if d.DeviceID != my && !d.Paused {
			peers = append(peers, d.DeviceID)
		}
	}
	for _, id := range peers {
		_ = s.C.PatchDevice(ctx, id, map[string]any{"paused": true})
	}
	time.Sleep(ReconnectPause)
	for _, id := range peers {
		_ = s.C.PatchDevice(ctx, id, map[string]any{"paused": false})
	}
	return name, nil
}

// MyName 返回本机名称。
func (s *Service) MyName(ctx context.Context) (string, string, error) {
	my, err := s.MyID(ctx)
	if err != nil {
		return "", "", err
	}
	devs, err := s.C.Devices(ctx)
	for _, d := range devs {
		if d.DeviceID == my {
			return my, d.Name, nil
		}
	}
	return my, "", err
}

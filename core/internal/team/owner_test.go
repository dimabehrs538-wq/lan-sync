package team

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

const (
	devA = "AAAAAAA-AAAAAAA-AAAAAAA-AAAAAAA-AAAAAAA-AAAAAAA-AAAAAAA-AAAAAAA" // 共享者
	devB = "BBBBBBB-BBBBBBB-BBBBBBB-BBBBBBB-BBBBBBB-BBBBBBB-BBBBBBB-BBBBBBB" // 成员
	devC = "CCCCCCC-CCCCCCC-CCCCCCC-CCCCCCC-CCCCCCC-CCCCCCC-CCCCCCC-CCCCCCC" // 另一位成员
)

// ownerST 模拟 Syncthing REST：一个文件夹，可设置 .teamsync.json 的 modifiedBy。
type ownerST struct {
	mu        sync.Mutex
	my        string
	folder    stclient.Folder
	globalBy  string
	localBy   string
	patches   []map[string]any
	localSize int64
	localMod  time.Time
}

func (f *ownerST) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch {
		case r.URL.Path == "/rest/system/status":
			_ = json.NewEncoder(w).Encode(map[string]string{"myID": f.my})
		case r.URL.Path == "/rest/config/folders":
			_ = json.NewEncoder(w).Encode([]stclient.Folder{f.folder})
		case strings.HasPrefix(r.URL.Path, "/rest/config/folders/") && r.Method == "GET":
			_ = json.NewEncoder(w).Encode(f.folder)
		case strings.HasPrefix(r.URL.Path, "/rest/config/folders/") && r.Method == "PATCH":
			b, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(b, &m)
			f.patches = append(f.patches, m)
			if typ, ok := m["type"].(string); ok {
				f.folder.Type = typ
			}
			if l, ok := m["label"].(string); ok {
				f.folder.Label = l
			}
		case r.URL.Path == "/rest/db/file":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"global": map[string]any{"modifiedBy": f.globalBy, "size": f.localSize, "modified": f.localMod},
				"local":  map[string]any{"modifiedBy": f.localBy, "size": f.localSize, "modified": f.localMod},
			})
		case r.URL.Path == "/rest/db/scan":
		default:
			t.Logf("未处理 %s %s", r.Method, r.URL.Path)
			w.WriteHeader(404)
		}
	}
}

// writeModeFrom 写入模式文件，并让数据库记录为由 by 设备修改（与磁盘一致）。
func (f *ownerST) writeModeFrom(t *testing.T, m ShareMode, by string) {
	t.Helper()
	if err := WriteMode(f.folder.Path, m); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(f.folder.Path, ModeFile))
	f.mu.Lock()
	f.globalBy, f.localBy, f.localSize, f.localMod = stclient.ShortID(by), stclient.ShortID(by), fi.Size(), fi.ModTime()
	f.mu.Unlock()
}

func newFake(t *testing.T, my, typ string, devs ...string) (*ownerST, *Service) {
	f := &ownerST{my: my, folder: stclient.Folder{ID: "F1", Label: "周报", Path: t.TempDir(), Type: typ}}
	for _, d := range devs {
		f.folder.Devices = append(f.folder.Devices, stclient.FolderDevice{DeviceID: d})
	}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	return f, &Service{C: stclient.New(srv.URL, "k")}
}

func TestShortID(t *testing.T) {
	if stclient.ShortID(devA) != "AAAAAAA" || stclient.ShortID("ABC") != "ABC" {
		t.Fatal(stclient.ShortID(devA))
	}
}

func TestOwnerStoreImmutable(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.json")
	st := OpenStore(p, t.TempDir())
	if _, ok := st.Owner("F"); ok {
		t.Fatal("初始应无记录")
	}
	_ = st.RecordNewFolderOwner("F", devA)
	if wrote, _ := st.RecordOwnerIfMissing("F", devB); wrote {
		t.Fatal("迁移不能覆盖已有记录")
	}
	if err := st.AssignOwner("F", devB); !errors.Is(err, ErrOwnerFixed) {
		t.Fatal("已确定的共享者不能手动修改", err)
	}
	if o, _ := OpenStore(p, "").Owner("F"); o != devA {
		t.Fatal("应持久化", o)
	}
	// 未知 → 可由用户手动指定一次
	_, _ = st.RecordOwnerIfMissing("G", "")
	if o, ok := st.Owner("G"); o != "" || !ok {
		t.Fatal(o, ok)
	}
	if err := st.AssignOwner("G", devA); err != nil {
		t.Fatal(err)
	}
	if err := st.AssignOwner("G", devB); !errors.Is(err, ErrOwnerFixed) {
		t.Fatal("指定后不能再改", err)
	}
	_ = st.ForgetOwner("G")
	if _, ok := st.Owner("G"); ok {
		t.Fatal("移除文件夹后应清除")
	}
}

func TestMemberCannotSeizeOwnership(t *testing.T) {
	// B 是成员（本机记录共享者 = A），协作文件夹中没有 .teamsync.json
	fk, svc := newFake(t, devB, "sendreceive", devA, devB, devC)
	ctx := context.Background()
	if err := svc.SetMode(ctx, "F1", ModeReadOnly, devA); !errors.Is(err, ErrNotOwner) {
		t.Fatal("成员不能切换模式", err)
	}
	if err := svc.SetMode(ctx, "F1", ModeReadOnly, ""); !errors.Is(err, ErrOwnerUnknown) {
		t.Fatal("共享者未知时谁都不能切换", err)
	}
	if _, exists := ReadMode(fk.folder.Path); exists || len(fk.patches) != 0 {
		t.Fatal("被拒绝时不能写模式文件或修改配置")
	}
	// 即使文件夹里已有（伪造的）“共享者是 B”的文件，也不能据此获得权限
	fk.writeModeFrom(t, ShareMode{Mode: ModeCollab, Owner: devB}, devB)
	if err := svc.SetMode(ctx, "F1", ModeReadOnly, devA); !errors.Is(err, ErrNotOwner) {
		t.Fatal("伪造文件不能授予权限", err)
	}
	// 本机记录的共享者是自己时可以切换
	fa, sa := newFake(t, devA, "sendreceive", devA, devB)
	if err := sa.SetMode(ctx, "F1", ModeReadOnly, devA); err != nil {
		t.Fatal(err)
	}
	if fa.folder.Type != "sendonly" {
		t.Fatal(fa.folder.Type)
	}
	if m, _ := ReadMode(fa.folder.Path); m.Owner != devA || m.Mode != ModeReadOnly {
		t.Fatalf("%+v", m)
	}
}

func TestSyncModesIgnoresForgedFile(t *testing.T) {
	ctx := context.Background()
	owner := func(string) string { return devA }
	run := func(svc *Service) (int, string) {
		var why string
		ch, err := svc.SyncModes(ctx, owner, func(_ stclient.Folder, r string) { why = r })
		if err != nil {
			t.Fatal(err)
		}
		return len(ch), why
	}
	// 1. 成员 B 自己把文件改成“共享者 B、只读”→ C 忽略（共享者不一致）
	fk, svc := newFake(t, devC, "sendreceive", devA, devB, devC)
	fk.writeModeFrom(t, ShareMode{Mode: ModeReadOnly, Owner: devB}, devB)
	if n, why := run(svc); n != 0 || fk.folder.Type != "sendreceive" || !strings.Contains(why, "不一致") {
		t.Fatal("共享者不一致应忽略", n, why)
	}
	// 2. B 伪造 owner=A 的文件 → 文件最后由 B 修改，忽略
	fk.writeModeFrom(t, ShareMode{Mode: ModeReadOnly, Owner: devA}, devB)
	if n, why := run(svc); n != 0 || fk.folder.Type != "sendreceive" || !strings.Contains(why, "不是共享者") {
		t.Fatal("非共享者修改的文件应忽略", n, why)
	}
	// 3. 全局版本来自 A，但本机有改动（本机版本由 C 修改）→ 忽略
	fk.writeModeFrom(t, ShareMode{Mode: ModeReadOnly, Owner: devA}, devA)
	fk.mu.Lock()
	fk.localBy = stclient.ShortID(devC)
	fk.mu.Unlock()
	if n, _ := run(svc); n != 0 {
		t.Fatal("本机版本不是共享者的应忽略")
	}
	// 4. 磁盘文件被改动但尚未扫描（与数据库记录不一致）→ 忽略
	fk.writeModeFrom(t, ShareMode{Mode: ModeReadOnly, Owner: devA}, devA)
	_ = WriteMode(fk.folder.Path, ShareMode{Mode: ModeReadOnly, Owner: devA, UpdatedAt: time.Now().Add(time.Hour)})
	if n, why := run(svc); n != 0 || !strings.Contains(why, "本机被改动") {
		t.Fatal("未扫描的本机改动应忽略", n, why)
	}
	// 5. 真正来自共享者 A → 跟随为仅接收
	fk.writeModeFrom(t, ShareMode{Mode: ModeReadOnly, Owner: devA}, devA)
	if n, _ := run(svc); n != 1 || fk.folder.Type != "receiveonly" {
		t.Fatal("来自共享者的设置应生效", n, fk.folder.Type)
	}
	// 6. 本机没有共享者记录 → 忽略
	fk.writeModeFrom(t, ShareMode{Mode: ModeCollab, Owner: devA}, devA)
	ch, _ := svc.SyncModes(ctx, func(string) string { return "" }, nil)
	if len(ch) != 0 || fk.folder.Type != "receiveonly" {
		t.Fatal("共享者未知时不应跟随")
	}
}

func TestDecideOwner(t *testing.T) {
	cases := []struct {
		name, typ string
		devs      []string
		created   bool
		fileOwner string
		want      string
	}{
		{"仅发送=本机", "sendonly", []string{devB, devA, devC}, false, "", devB},
		{"本机新建的协作文件夹", "sendreceive", []string{devB, devA, devC}, true, devA, devB},
		{"可信模式文件指向他人", "receiveonly", []string{devB, devA, devC}, false, devA, devA},
		{"文件自称本机不采信", "sendreceive", []string{devB, devA, devC}, false, devB, ""},
		{"文件指向非成员不采信", "receiveonly", []string{devB, devA}, false, devC, devA},
		{"只有一位邀请人", "sendreceive", []string{devB, devA}, false, "", devA},
		{"多人且无信息=未知", "sendreceive", []string{devB, devA, devC}, false, "", ""},
	}
	for _, c := range cases {
		if got := decideOwner(c.typ, devB, c.devs, c.created, c.fileOwner); got != c.want {
			t.Errorf("%s: got %q want %q", c.name, got, c.want)
		}
	}
}

func TestMigrateOwners(t *testing.T) {
	ctx := context.Background()
	// 成员 B 的只读文件夹，有来自 A 的可信模式文件
	fk, svc := newFake(t, devB, "receiveonly", devA, devB, devC)
	fk.writeModeFrom(t, ShareMode{Mode: ModeReadOnly, Owner: devA}, devA)
	st := OpenStore(filepath.Join(t.TempDir(), "s.json"), t.TempDir())
	if logs := svc.MigrateOwners(ctx, st); len(logs) != 1 {
		t.Fatal(logs)
	}
	if o, _ := st.Owner("F1"); o != devA {
		t.Fatal(o)
	}
	// 再次迁移不会改变（即使文件被改成指向 C）
	fk.writeModeFrom(t, ShareMode{Mode: ModeReadOnly, Owner: devC}, devC)
	if logs := svc.MigrateOwners(ctx, st); len(logs) != 0 {
		t.Fatal(logs)
	}
	if o, _ := st.Owner("F1"); o != devA {
		t.Fatal("共享者不可变", o)
	}
	// 多人协作且无信息 → 未知
	_, svc2 := newFake(t, devB, "sendreceive", devA, devB, devC)
	st2 := OpenStore(filepath.Join(t.TempDir(), "s.json"), t.TempDir())
	svc2.MigrateOwners(ctx, st2)
	if o, ok := st2.Owner("F1"); o != "" || !ok {
		t.Fatal("应记为未知", o, ok)
	}
	// 本机新建（有 .stignore）→ 本机
	f3, svc3 := newFake(t, devA, "sendreceive", devA, devB, devC)
	_ = WriteIgnores(f3.folder.Path)
	st3 := OpenStore(filepath.Join(t.TempDir(), "s.json"), t.TempDir())
	svc3.MigrateOwners(ctx, st3)
	if o, _ := st3.Owner("F1"); o != devA {
		t.Fatal(o)
	}
}

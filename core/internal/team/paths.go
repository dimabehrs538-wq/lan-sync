package team

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

// 文件夹位置安全检查（v0.2.6）：团队文件夹之间不能相同或互相嵌套，
// 也不能直接使用磁盘根目录、用户主目录、桌面、下载目录或“接收文件夹的默认位置”本身。
// 否则一个文件夹的内容会被当作另一个文件夹的内容同步出去，造成文件互相串改。

var isWindows = runtime.GOOS == "windows"

var driveRe = regexp.MustCompile(`^[a-zA-Z]:`)

// normPath 把路径规范化为便于比较的形式：统一用 "/"，去掉 "."/".."、末尾斜杠；Windows 下不区分大小写。
// win 参数让 Windows 规则在任何平台都可测试。
func normPath(p string, win bool) string {
	if win {
		p = strings.ReplaceAll(p, `\`, "/")
	}
	unc := win && strings.HasPrefix(p, "//")
	p = path.Clean(p)
	if unc {
		p = "/" + p // \\server\share
	}
	if win {
		p = strings.ToLower(p)
		if driveRe.MatchString(p) && len(p) == 2 {
			p += "/" // "c:" -> "c:/"
		}
	}
	return p
}

// within 判断 child 是否等于 parent 或位于 parent 之内（均为 normPath 结果）。
func within(child, parent string) bool {
	if child == parent {
		return true
	}
	if !strings.HasSuffix(parent, "/") {
		parent += "/"
	}
	return strings.HasPrefix(child, parent)
}

func isRoot(n string, win bool) bool {
	if n == "/" {
		return true
	}
	if win {
		if driveRe.MatchString(n) && len(n) <= 3 {
			return true
		}
		// \\server\share 共享根目录
		if strings.HasPrefix(n, "//") && strings.Count(strings.TrimPrefix(n, "//"), "/") <= 1 {
			return true
		}
	}
	return false
}

// ResolvePath 返回绝对路径，并尽量解析符号链接（路径可能尚不存在：解析最长的已存在前缀）。
func ResolvePath(p string) string {
	if strings.HasPrefix(p, "~") {
		if h, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(h, strings.TrimPrefix(p, "~"))
		}
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	rest := ""
	cur := p
	for {
		if r, err := filepath.EvalSymlinks(cur); err == nil {
			r = longPath(r) // Windows：把 8.3 短文件名（如 RUNNER~1）展开为长文件名，保证两边可比较
			if rest != "" {
				return filepath.Join(r, rest)
			}
			return r
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return p
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}

// PathRules 描述一次位置检查所需的上下文。
type PathRules struct {
	Win       bool
	Home      string // 用户主目录
	BaseDir   string // 接收文件夹的默认位置
	Existing  []stclient.Folder
	ExcludeID string // 检查该文件夹自身的新位置时排除自己
}

// ErrPathNotAllowed 表示位置不安全，错误信息为中文。
var ErrPathNotAllowed = errors.New("位置不可用")

type pathError struct{ msg string }

func (e *pathError) Error() string { return e.msg }
func (e *pathError) Unwrap() error { return ErrPathNotAllowed }

func folderName(f stclient.Folder) string {
	if n, _ := ParseLabel(f.Label); n != "" {
		return n
	}
	return f.ID
}

// Check 检查 p（已解析的绝对路径）作为团队文件夹位置是否安全；resolve 为解析函数（测试中可用恒等函数）。
func (r PathRules) Check(p string, resolve func(string) string) error {
	if strings.TrimSpace(p) == "" {
		return &pathError{"路径不能为空"}
	}
	if resolve == nil {
		resolve = func(s string) string { return s }
	}
	n := normPath(resolve(p), r.Win)
	if isRoot(n, r.Win) {
		return &pathError{"不能直接使用磁盘根目录作为团队文件夹，请在其中新建一个子文件夹"}
	}
	if r.Home != "" {
		h := normPath(resolve(r.Home), r.Win)
		special := map[string]string{h: "用户主目录"}
		sep := "/"
		if r.Win {
			sep = `\`
		}
		for _, d := range []string{"Desktop", "Downloads"} {
			special[normPath(resolve(strings.TrimRight(r.Home, `/\`)+sep+d), r.Win)] = map[string]string{"Desktop": "桌面", "Downloads": "下载"}[d] + "目录"
		}
		if r.Win { // 桌面常被 OneDrive 重定向
			special[normPath(r.Home+`\OneDrive\Desktop`, true)] = "桌面目录"
		}
		if what, ok := special[n]; ok {
			return &pathError{fmt.Sprintf("不能直接使用%s作为团队文件夹，请在其中新建一个子文件夹", what)}
		}
	}
	if r.BaseDir != "" && n == normPath(resolve(r.BaseDir), r.Win) {
		return &pathError{"不能直接使用“接收文件夹的默认位置”本身，请使用其中以文件夹名命名的子文件夹"}
	}
	for _, f := range r.Existing {
		if f.ID == r.ExcludeID || f.Path == "" {
			continue
		}
		e := normPath(resolve(f.Path), r.Win)
		switch {
		case n == e:
			return &pathError{fmt.Sprintf("该位置已被团队文件夹「%s」使用，请选择其他位置", folderName(f))}
		case within(n, e):
			return &pathError{fmt.Sprintf("该位置位于团队文件夹「%s」之内。文件夹不能互相嵌套，否则内容会互相同步，请选择其他位置", folderName(f))}
		case within(e, n):
			return &pathError{fmt.Sprintf("该位置包含了团队文件夹「%s」。文件夹不能互相嵌套，否则内容会互相同步，请选择其他位置", folderName(f))}
		}
	}
	return nil
}

// Overlap 描述一个与更早添加的文件夹重叠的文件夹。
type Overlap struct {
	With      string `json:"with"`      // 对方文件夹 ID
	WithLabel string `json:"withLabel"` // 对方名称
	Kind      string `json:"kind"`      // same | inside | contains
}

// FindOverlaps 找出位置相同或互相嵌套的文件夹；后添加的一方记为“有问题”。
// addedAt 返回文件夹的添加时间（为 nil 或时间相同时按传入顺序）。
func FindOverlaps(folders []stclient.Folder, win bool, resolve func(string) string, addedAt func(stclient.Folder) time.Time) map[string]Overlap {
	if resolve == nil {
		resolve = func(s string) string { return s }
	}
	folders = append([]stclient.Folder{}, folders...)
	if addedAt != nil {
		ts := map[string]time.Time{}
		for _, f := range folders {
			ts[f.ID] = addedAt(f)
		}
		sort.SliceStable(folders, func(i, j int) bool { return ts[folders[i].ID].Before(ts[folders[j].ID]) })
	}
	norm := make([]string, len(folders))
	for i, f := range folders {
		norm[i] = normPath(resolve(f.Path), win)
	}
	out := map[string]Overlap{}
	for j := range folders {
		for i := 0; i < j; i++ {
			if folders[i].Path == "" || folders[j].Path == "" {
				continue
			}
			kind := ""
			switch {
			case norm[i] == norm[j]:
				kind = "same"
			case within(norm[j], norm[i]):
				kind = "inside"
			case within(norm[i], norm[j]):
				kind = "contains"
			}
			if kind != "" {
				out[folders[j].ID] = Overlap{With: folders[i].ID, WithLabel: folderName(folders[i]), Kind: kind}
				break
			}
		}
	}
	return out
}

// OverlapMessage 返回重叠问题的中文说明。
func OverlapMessage(o Overlap) string {
	rel := map[string]string{"same": "与团队文件夹「%s」使用了同一个位置", "inside": "位于团队文件夹「%s」之内", "contains": "包含了团队文件夹「%s」"}[o.Kind]
	return fmt.Sprintf("此文件夹"+rel+"，两个文件夹的内容会互相同步、混在一起。为防止文件被改乱，已暂停此文件夹的同步。请点击“重新选择位置”为它指定一个单独的空文件夹（原位置中多出来的文件请确认后手动整理）。", o.WithLabel)
}

var reservedWin = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[1-9]|lpt[1-9])(\..*)?$`)

// SafeName 把文件夹名称转换为 Windows 下合法的目录名：替换非法字符、去掉控制字符及末尾的点和空格、避开保留名。
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
	out := strings.TrimRight(strings.TrimSpace(string(r)), ". ")
	if out == "" || strings.Trim(out, ".") == "" {
		return "folder"
	}
	if reservedWin.MatchString(out) {
		out = "_" + out
	}
	if len([]rune(out)) > 100 {
		out = string([]rune(out)[:100])
	}
	return out
}

// SuggestPath 返回接受邀请时的默认位置：<默认位置>/<文件夹名>；
// 若该目录已存在且非空、或与已有文件夹重叠，则依次尝试 “名称 (2)”、“名称 (3)”……
func SuggestPath(base, label string, existing []stclient.Folder) string {
	name := SafeName(label)
	rules := PathRules{Win: isWindows, Existing: existing}
	for i := 1; i < 1000; i++ {
		n := name
		if i > 1 {
			n = fmt.Sprintf("%s (%d)", name, i)
		}
		p := filepath.Join(base, n)
		if rules.Check(p, ResolvePath) != nil {
			continue
		}
		if fi, err := os.Stat(p); err == nil && (!fi.IsDir() || !dirEmpty(p)) {
			continue
		}
		return p
	}
	return filepath.Join(base, name)
}

// CheckPath 用当前配置检查位置；excludeID 为正在修改位置的文件夹。
func (s *Service) CheckPath(folders []stclient.Folder, p, baseDir, excludeID string) error {
	home, _ := os.UserHomeDir()
	return PathRules{Win: isWindows, Home: home, BaseDir: baseDir, Existing: folders, ExcludeID: excludeID}.Check(p, ResolvePath)
}

// PathInfo 返回位置是否存在、是否为空、包含多少项（用于界面上“目录非空”确认）。
type PathInfo struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Exists  bool   `json:"exists"`
	Empty   bool   `json:"empty"`
	Entries int    `json:"entries"`
}

func InspectPath(p string) PathInfo {
	info := PathInfo{OK: true, Empty: true}
	ents, err := os.ReadDir(p)
	if err != nil {
		if fi, e := os.Stat(p); e == nil && !fi.IsDir() {
			info.Exists, info.Empty = true, false
		}
		return info
	}
	info.Exists = true
	for _, e := range ents {
		n := e.Name()
		if n == ".stfolder" || n == ".stignore" || n == ".stversions" || strings.HasPrefix(n, ".syncthing.") {
			continue
		}
		info.Entries++
	}
	info.Empty = info.Entries == 0
	return info
}

// MarkerTime 返回文件夹同步标记 .stfolder 的修改时间（近似为添加时间，用于旧版本未记录的文件夹）。
func MarkerTime(f stclient.Folder) time.Time {
	if fi, err := os.Stat(filepath.Join(f.Path, markerName(f))); err == nil {
		return fi.ModTime()
	}
	return time.Time{}
}

// RecordAdded 记录文件夹的添加时间（已记录的不覆盖）。
func (st *Store) RecordAdded(id string, t time.Time) {
	if !st.Get().AddedAt[id].IsZero() {
		return
	}
	_ = st.Update(func(s *Settings) {
		if s.AddedAt == nil {
			s.AddedAt = map[string]time.Time{}
		}
		if s.AddedAt[id].IsZero() {
			s.AddedAt[id] = t
		}
	})
}

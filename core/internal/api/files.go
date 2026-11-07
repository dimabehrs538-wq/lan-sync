package api

// 文件夹内文件浏览 / 导入 / 导出：手机上没有资源管理器可以直接打开团队文件夹（iOS 沙盒、Android 分区存储），
// 移动端界面通过这些接口浏览文件、从系统文件选择器导入文件、把文件交给系统分享/打开。桌面端同样可用。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileEntry 是文件夹中的一个文件或子目录。
type FileEntry struct {
	Name    string    `json:"name"`
	Path    string    `json:"path"` // 相对文件夹根目录，使用 /
	Abs     string    `json:"abs"`  // 本机绝对路径（交给系统分享/打开）
	Dir     bool      `json:"dir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

// 引擎内部文件，不在文件列表中显示、不允许通过接口写入
func internalName(n string) bool {
	return n == ".stfolder" || n == ".stversions" || n == ".stignore" || n == ".teamsync.json" || strings.HasPrefix(n, ".syncthing.") || strings.HasPrefix(n, "~syncthing~")
}

var errBadPath = errors.New("文件路径不正确")

// cleanRel 校验并规范化相对路径：不能为绝对路径、不能包含 ..、不能指向引擎内部文件。
func cleanRel(p string) (string, error) {
	p = strings.ReplaceAll(strings.TrimSpace(p), `\`, "/")
	if p == "" || p == "." || p == "/" {
		return "", nil
	}
	if strings.HasPrefix(p, "/") || filepath.IsAbs(p) || strings.Contains(p, "\x00") {
		return "", errBadPath
	}
	parts := strings.Split(p, "/")
	out := parts[:0]
	for _, s := range parts {
		switch s {
		case "", ".":
			continue
		case "..":
			return "", errBadPath
		}
		if internalName(s) {
			return "", errBadPath
		}
		out = append(out, s)
	}
	return strings.Join(out, "/"), nil
}

// within 确认 p（解析符号链接后）仍在文件夹根目录内：同步来的符号链接不能把读写引到文件夹之外。
func within(root, p string) error {
	rr, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	rp, err := filepath.EvalSymlinks(p)
	if err != nil {
		return err
	}
	if rp != rr && !strings.HasPrefix(rp, rr+string(filepath.Separator)) {
		return errBadPath
	}
	return nil
}

func (s *Server) folderRoot(r *http.Request) (root, ftype string, err error) {
	f, err := s.C.Folder(r.Context(), r.PathValue("id"))
	if err != nil {
		return "", "", err
	}
	if f.Path == "" {
		return "", "", errors.New("文件夹没有本地位置")
	}
	return filepath.Clean(f.Path), f.Type, nil
}

// GET /api/folders/{id}/files?dir=相对目录
func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) {
	root, _, err := s.folderRoot(r)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	rel, err := cleanRel(r.URL.Query().Get("dir"))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if err := within(root, dir); err != nil && !os.IsNotExist(err) {
		writeErr(w, 400, errBadPath)
		return
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			writeErr(w, 404, errors.New("目录不存在"))
		} else {
			writeErr(w, 500, fmt.Errorf("无法读取目录：%v", err))
		}
		return
	}
	list := []FileEntry{}
	for _, e := range ents {
		if internalName(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		p := e.Name()
		if rel != "" {
			p = rel + "/" + p
		}
		fe := FileEntry{Name: e.Name(), Path: p, Abs: filepath.Join(dir, e.Name()), Dir: info.IsDir(), ModTime: info.ModTime()}
		if !fe.Dir {
			fe.Size = info.Size()
		}
		list = append(list, fe)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Dir != list[j].Dir {
			return list[i].Dir
		}
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	writeJSON(w, map[string]any{"dir": rel, "root": root, "entries": list})
}

// uniqueName 在目录中为 name 找一个不冲突的名字：报告.docx → 报告 (2).docx
func uniqueName(dir, name string) string {
	if _, err := os.Lstat(filepath.Join(dir, name)); os.IsNotExist(err) {
		return name
	}
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 2; ; i++ {
		n := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Lstat(filepath.Join(dir, n)); os.IsNotExist(err) {
			return n
		}
	}
}

func safeFileName(n string) (string, error) {
	n = strings.TrimSpace(strings.ReplaceAll(n, `\`, "/"))
	n = n[strings.LastIndex(n, "/")+1:]
	n = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"|?*`, r) {
			return '_'
		}
		return r
	}, n)
	if n == "" || n == "." || n == ".." || internalName(n) {
		return "", errors.New("文件名不正确")
	}
	return n, nil
}

// POST /api/folders/{id}/files?dir=相对目录&name=文件名[&overwrite=1]，请求体为文件内容。
// 默认不覆盖已有文件（自动改名为“名称 (2)”）；先写临时文件再改名，引擎不会同步半个文件。
func (s *Server) uploadFile(w http.ResponseWriter, r *http.Request) {
	root, ftype, err := s.folderRoot(r)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	if ftype == "receiveonly" {
		writeErr(w, 403, errors.New("这是只读共享文件夹，本机添加的文件不会同步给其他成员"))
		return
	}
	q := r.URL.Query()
	rel, err := cleanRel(q.Get("dir"))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	name, err := safeFileName(q.Get("name"))
	if err != nil {
		writeErr(w, 400, err)
		return
	}
	dir := filepath.Join(root, filepath.FromSlash(rel))
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		if rel == "" {
			writeErr(w, 409, errors.New("文件夹位置不存在，请先修复文件夹"))
			return
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeErr(w, 500, fmt.Errorf("无法创建目录：%v", err))
			return
		}
	}
	if err := within(root, dir); err != nil {
		writeErr(w, 400, errBadPath)
		return
	}
	if q.Get("overwrite") != "1" {
		name = uniqueName(dir, name)
	} else if fi, err := os.Lstat(filepath.Join(dir, name)); err == nil && fi.IsDir() {
		writeErr(w, 409, errors.New("已存在同名文件夹"))
		return
	}
	tmp, err := os.CreateTemp(dir, ".teamsync-upload-*.tmp")
	if err != nil {
		writeErr(w, 500, fmt.Errorf("无法写入文件：%v", err))
		return
	}
	n, cerr := io.Copy(tmp, r.Body)
	if err := tmp.Close(); cerr == nil {
		cerr = err
	}
	if cerr != nil {
		os.Remove(tmp.Name())
		writeErr(w, 500, fmt.Errorf("写入文件失败：%v", cerr))
		return
	}
	dst := filepath.Join(dir, name)
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		writeErr(w, 500, fmt.Errorf("保存文件失败：%v", err))
		return
	}
	_ = os.Chmod(dst, 0o644)
	p := name
	if rel != "" {
		p = rel + "/" + name
	}
	// 立即扫描该路径，不必等待文件监视
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = s.C.Do(ctx, "POST", "/rest/db/scan?folder="+url.QueryEscape(r.PathValue("id"))+"&sub="+url.QueryEscape(p), nil, nil, nil)
	}()
	writeJSON(w, map[string]any{"ok": true, "name": name, "path": p, "abs": dst, "size": n})
}

// GET /api/folders/{id}/file?path=相对路径[&download=1]：读取文件内容（导出/分享/预览）。
func (s *Server) downloadFile(w http.ResponseWriter, r *http.Request) {
	root, _, err := s.folderRoot(r)
	if err != nil {
		writeErr(w, 404, err)
		return
	}
	rel, err := cleanRel(r.URL.Query().Get("path"))
	if err != nil || rel == "" {
		writeErr(w, 400, errBadPath)
		return
	}
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := within(root, p); err != nil {
		writeErr(w, 404, errors.New("文件不存在"))
		return
	}
	f, err := os.Open(p)
	if err != nil {
		writeErr(w, 404, errors.New("文件不存在"))
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		writeErr(w, 400, errors.New("不是文件"))
		return
	}
	name := filepath.Base(p)
	if ct := mime.TypeByExtension(filepath.Ext(name)); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	disp := "inline"
	if r.URL.Query().Get("download") == "1" {
		disp = "attachment"
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
	http.ServeContent(w, r, name, fi.ModTime(), f)
}

package team

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

// Problem 描述文件夹的可修复故障（标记目录丢失 / 路径丢失）。
type Problem struct {
	Kind       string `json:"kind"`    // markerMissing | pathMissing | other
	Message    string `json:"message"` // 中文说明
	PathExists bool   `json:"pathExists"`
	PathEmpty  bool   `json:"pathEmpty"`
	InGitRepo  bool   `json:"inGitRepo"` // 位于 git 仓库内，提示把 .stfolder 加入 .gitignore
}

func markerName(f stclient.Folder) string {
	if f.MarkerName != "" {
		return f.MarkerName
	}
	return ".stfolder"
}

// dirEmpty 判断目录是否为空（忽略 Syncthing 自己的元数据目录）。
func dirEmpty(p string) bool {
	d, err := os.Open(p)
	if err != nil {
		return true
	}
	defer d.Close()
	for {
		names, err := d.Readdirnames(32)
		for _, n := range names {
			if n != ".stfolder" && n != ".stversions" && n != ".stignore" {
				return false
			}
		}
		if err == io.EOF || err != nil {
			return true
		}
	}
}

func inGitRepo(p string) bool {
	for d := filepath.Clean(p); ; {
		if _, err := os.Stat(filepath.Join(d, ".git")); err == nil {
			return true
		}
		up := filepath.Dir(d)
		if up == d {
			return false
		}
		d = up
	}
}

// Diagnose 结合 Syncthing 的错误信息和本地文件系统检查文件夹健康状况；健康时返回 nil。
func Diagnose(f stclient.Folder, stErr string) *Problem {
	if f.Paused {
		return nil
	}
	low := strings.ToLower(stErr)
	fi, err := os.Stat(f.Path)
	exists := err == nil && fi.IsDir()
	switch {
	case !exists || strings.Contains(low, "folder path missing"):
		if exists { // 引擎尚未刷新状态
			break
		}
		return &Problem{Kind: "pathMissing", Message: "本地文件夹不存在（可能被移动、改名或删除，或所在磁盘未连接）。请重新选择位置，或移除该文件夹。"}
	}
	_, merr := os.Stat(filepath.Join(f.Path, markerName(f)))
	if errors.Is(merr, os.ErrNotExist) || strings.Contains(low, "marker missing") {
		if merr == nil { // 已修复，等待引擎刷新
			return nil
		}
		return &Problem{Kind: "markerMissing", PathExists: true, PathEmpty: dirEmpty(f.Path), InGitRepo: inGitRepo(f.Path),
			Message: "文件夹中的同步标记 " + markerName(f) + " 被删除了（常见原因：git clean、清理工具或 AI 工具删除了隐藏文件）。为防止误删其他成员的文件，同步已暂停。确认本地文件完好后点击“修复”即可恢复。"}
	}
	if stErr != "" {
		return &Problem{Kind: "other", PathExists: true, Message: TranslateEngineError(stErr)}
	}
	return nil
}

// TranslateEngineError 把 Syncthing 的常见英文错误翻译成中文。
func TranslateEngineError(msg string) string {
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "marker missing"):
		return "同步标记 .stfolder 丢失（可能被 git clean 或清理工具删除），同步已暂停，请在文件夹卡片上点击“修复”"
	case strings.Contains(low, "folder path missing"), strings.Contains(low, "path does not exist"):
		return "本地文件夹不存在（可能被移动、删除或磁盘未连接），请重新选择位置或移除该文件夹"
	case strings.Contains(low, "insufficient space"), strings.Contains(low, "not enough space"):
		return "磁盘空间不足"
	case strings.Contains(low, "permission denied"), strings.Contains(low, "access is denied"):
		return "没有权限访问该文件夹"
	case strings.Contains(low, "folder is paused"):
		return "文件夹已暂停"
	}
	return msg
}

var ErrPathMissing = errors.New("本地文件夹不存在，无法修复，请重新选择位置或移除")
var ErrNeedConfirm = errors.New("文件夹是空的，修复后可能导致其他成员的文件被删除，请确认后再修复")

// Repair 重建 .stfolder 标记并重新扫描。目录为空时需 force=true。
func (s *Service) Repair(ctx context.Context, id string, force bool) error {
	f, err := s.C.Folder(ctx, id)
	if err != nil {
		return err
	}
	if fi, err := os.Stat(f.Path); err != nil || !fi.IsDir() {
		return ErrPathMissing
	}
	if dirEmpty(f.Path) && !force {
		return ErrNeedConfirm
	}
	if err := os.MkdirAll(filepath.Join(f.Path, markerName(f)), 0o755); err != nil {
		return err
	}
	hideMarker(filepath.Join(f.Path, markerName(f)))
	return s.restartFolder(ctx, f)
}

// restartFolder 通过暂停/恢复让引擎立即重新检查文件夹，然后扫描。
func (s *Service) restartFolder(ctx context.Context, f stclient.Folder) error {
	if !f.Paused {
		if err := s.C.PatchFolder(ctx, f.ID, map[string]any{"paused": true}); err != nil {
			return err
		}
		if err := s.C.PatchFolder(ctx, f.ID, map[string]any{"paused": false}); err != nil {
			return err
		}
	}
	_ = s.C.Scan(ctx, f.ID, "") // 文件夹刚重启时扫描可能被拒绝，引擎随后会自动扫描
	return nil
}

// Relocate 把文件夹指向新的本地路径（目录不存在时创建）。
func (s *Service) Relocate(ctx context.Context, id, path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("路径不能为空")
	}
	f, err := s.C.Folder(ctx, id)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(path, markerName(f)), 0o755); err != nil {
		return err
	}
	hideMarker(filepath.Join(path, markerName(f)))
	if err := s.C.PatchFolder(ctx, id, map[string]any{"path": path}); err != nil {
		return err
	}
	f.Path = path
	return s.restartFolder(ctx, f)
}

package mobilecore

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/lan-sync/teamsync/core/internal/daemon"
	"github.com/lan-sync/teamsync/core/internal/rotlog"
	"github.com/lan-sync/teamsync/core/internal/trash"
)

// Config 对应桌面 `teamsyncd serve` 的参数。
type Config struct {
	Home       string // App 私有数据目录下的 TeamSync（syncthing 配置在 Home/st，与桌面布局一致）
	FoldersDir string // 接收邀请的默认存放目录（App 沙盒内，用户可在设置中修改）
	APIPort    int
	Token      string
	Listen     string // 测试用：同步监听地址；空 = syncthing 默认（tcp/quic 22000）
	// NoTrash: 手机没有系统回收站，“移除并删除本地文件”只能永久删除（界面会给出选项）
	NoTrash bool
}

type Node struct {
	d   *daemon.Daemon
	eng *Engine
	log io.Closer
}

func (n *Node) Port() int     { return n.d.Port }
func (n *Node) Token() string { return n.d.Token }

var (
	mu      sync.Mutex
	current *Node
)

// Start 启动嵌入的 syncthing 与本地 API；同一进程只运行一个节点。
func Start(cfg Config) (*Node, error) {
	mu.Lock()
	defer mu.Unlock()
	if current != nil {
		return current, nil
	}
	if cfg.Home == "" || cfg.FoldersDir == "" {
		return nil, errors.New("Home 和 FoldersDir 不能为空")
	}
	if err := os.MkdirAll(cfg.Home, 0o700); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.FoldersDir, 0o755); err != nil {
		return nil, err
	}
	if cfg.NoTrash {
		trash.TrashDir = func() (string, error) { return "", errors.New("手机上没有系统回收站") }
	}
	var closer io.Closer
	if cw, err := rotlog.Open(filepath.Join(cfg.Home, "teamsyncd.log"), 5<<20, 1); err == nil {
		closer = cw
		log.SetOutput(io.MultiWriter(os.Stderr, cw))
	}
	eng, err := StartEngine(filepath.Join(cfg.Home, "st"))
	if err != nil {
		return nil, err
	}
	d, err := daemon.Start(daemon.Options{Home: cfg.Home, FoldersDir: cfg.FoldersDir, Listen: cfg.Listen, APIPort: cfg.APIPort, Token: cfg.Token, Engine: eng})
	if err != nil {
		eng.Stop(10 * time.Second)
		return nil, err
	}
	log.Printf("移动端内核已启动：本地 API http://127.0.0.1:%d", d.Port)
	current = &Node{d: d, eng: eng, log: closer}
	return current, nil
}

// Stop 先停引擎再关闭本地 API。
func Stop() {
	mu.Lock()
	defer mu.Unlock()
	if current == nil {
		return
	}
	current.d.Shutdown(10 * time.Second)
	if current.log != nil {
		current.log.Close()
	}
	current = nil
}

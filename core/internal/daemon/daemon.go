// Package daemon 组装团队同步内核：连接同步引擎（桌面端为 syncthing 子进程，移动端为进程内嵌入的 syncthing），
// 应用内网默认配置并在 127.0.0.1 上提供给前端的本地 HTTP API + SSE。
// 桌面 `teamsyncd serve` 与移动端内核（core/mobile）共用这一段逻辑，保证两端 API/行为一致。
package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/lan-sync/teamsync/core/internal/activity"
	"github.com/lan-sync/teamsync/core/internal/api"
	"github.com/lan-sync/teamsync/core/internal/engine"
	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
)

// Engine 是同步引擎（syncthing）的抽象：已启动且 REST 可用后交给 Start。
type Engine interface {
	BaseURL() string
	APIKey() string
	State() string
	SetOnState(func(string))
	Stop(timeout time.Duration) error
}

type Options struct {
	Home       string // 数据目录（settings.json、activity.json 等）
	FoldersDir string // 接收邀请默认存放目录（首次运行写入设置）
	Listen     string // 同步监听地址（逗号分隔，空=默认）
	APIPort    int    // 本地 API 端口，0=自动
	Token      string // 本地 API token，空=随机
	Engine     Engine
}

type Daemon struct {
	Port   int
	Token  string
	Server *api.Server
	eng    Engine
	hs     *http.Server
	cancel context.CancelFunc
}

// Start 应用默认配置并开始提供本地 API（非阻塞）。Engine 必须已经启动。
func Start(opts Options) (*Daemon, error) {
	if opts.Engine == nil {
		return nil, errors.New("daemon: Engine 不能为空")
	}
	eng := opts.Engine
	ctx, cancel := context.WithCancel(context.Background())
	c := stclient.New(eng.BaseURL(), eng.APIKey())
	ts := &team.Service{C: c}
	if err := ts.ApplyLANDefaults(ctx); err != nil {
		log.Printf("应用内网默认配置失败: %v", err)
	}
	if opts.Listen != "" {
		if err := c.PatchOptions(ctx, map[string]any{"listenAddresses": strings.Split(opts.Listen, ",")}); err != nil {
			log.Printf("设置监听地址失败: %v", err)
		}
	}
	if err := ts.DisableAutoAccept(ctx); err != nil {
		log.Printf("关闭自动接收失败: %v", err)
	}
	store := team.OpenStore(filepath.Join(opts.Home, "settings.json"), opts.FoldersDir)
	if err := ts.MigrateRemarks(ctx, store); err != nil {
		log.Printf("迁移成员备注: %v", err)
	}
	if err := ts.ApplyFolderDefaults(ctx, store.Get().DefaultBaseDir); err != nil {
		log.Printf("设置文件夹默认值失败: %v", err)
	}
	token := opts.Token
	if token == "" {
		token = engine.GenerateAPIKey()
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", opts.APIPort))
	if err != nil {
		cancel()
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := api.New(c, token, eng.State, store)
	srv.Activity = activity.Open(filepath.Join(opts.Home, "activity.json"))
	eng.SetOnState(func(s string) {
		srv.Publish(api.UIEvent{Type: "EngineState", Time: time.Now(), Data: map[string]string{"state": s}})
	})
	go srv.RunEventBridge(ctx)
	go srv.RunOverlapGuard(ctx, api.GuardInterval())
	hs := &http.Server{Handler: srv.Handler()}
	go hs.Serve(ln)
	return &Daemon{Port: port, Token: token, Server: srv, eng: eng, hs: hs, cancel: cancel}, nil
}

// Shutdown 先停同步引擎，再关闭本地 API：API 端口关闭即表示引擎已退出（桌面安装更新时据此判断）。
func (d *Daemon) Shutdown(timeout time.Duration) {
	d.cancel()
	d.eng.Stop(timeout)
	sctx, scancel := context.WithTimeout(context.Background(), 3*time.Second)
	d.hs.Shutdown(sctx)
	scancel()
	d.Server.Activity.Flush()
}

// ProcessEngine 把桌面端的 syncthing 子进程引擎适配为 Engine。
type ProcessEngine struct{ *engine.Engine }

func (p ProcessEngine) State() string { return string(p.Engine.State()) }
func (p ProcessEngine) SetOnState(f func(string)) {
	p.Engine.OnState = func(s engine.State) { f(string(s)) }
}

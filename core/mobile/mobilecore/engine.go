// Package mobilecore 是 Android/iOS 端的团队同步内核：在 App 进程内嵌入官方 Syncthing（与桌面端捆绑的
// syncthing 可执行文件同一版本、同一 BEP 协议），并复用桌面内核的 internal/daemon 提供同样的本地 HTTP API。
// 手机系统不允许（iOS）或不适合（Android）再拉起子进程，因此这里不使用桌面的 syncthing/teamsyncd sidecar。
package mobilecore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/lan-sync/teamsync/core/internal/team"
	"github.com/syncthing/syncthing/lib/config"
	"github.com/syncthing/syncthing/lib/events"
	"github.com/syncthing/syncthing/lib/locations"
	"github.com/syncthing/syncthing/lib/svcutil"
	"github.com/syncthing/syncthing/lib/syncthing"
)

// 与 syncthing 命令行默认值一致
const (
	dbDeleteRetention     = 10920 * time.Hour
	dbMaintenanceInterval = 8 * time.Hour
)

// Engine 在进程内运行 syncthing.App，REST 只监听 127.0.0.1 随机端口，API Key 每次启动随机。
type Engine struct {
	home    string
	port    int
	apiKey  string
	mu      sync.Mutex
	state   string
	onState func(string)
	app     *syncthing.App
	cancel  context.CancelFunc
	stopped bool
	done    chan struct{}
}

var startOnce sync.Mutex // syncthing 的 locations 是进程级全局状态，同一时间只允许一个引擎

func randomKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// StartEngine 启动嵌入的 syncthing 并等待 REST 健康检查通过。
func StartEngine(home string) (*Engine, error) {
	if !startOnce.TryLock() {
		return nil, errors.New("同步引擎已在运行")
	}
	ok := false
	defer func() {
		if !ok {
			startOnce.Unlock()
		}
	}()
	if err := os.MkdirAll(home, 0o700); err != nil {
		return nil, err
	}
	// 等同桌面端 `syncthing serve --home=<home>`
	if err := locations.SetBaseDir(locations.ConfigBaseDir, home); err != nil {
		return nil, err
	}
	if err := locations.SetBaseDir(locations.DataBaseDir, home); err != nil {
		return nil, err
	}
	os.Setenv("STNOUPGRADE", "1")
	port, err := freePort()
	if err != nil {
		return nil, err
	}
	e := &Engine{home: home, port: port, apiKey: randomKey(), state: "starting", done: make(chan struct{})}
	if err := e.launch(); err != nil {
		return nil, err
	}
	ok = true // 之后由 supervise 在引擎最终停止时释放 startOnce
	go e.supervise()
	if err := e.waitHealthy(60 * time.Second); err != nil {
		e.Stop(10 * time.Second)
		return nil, err
	}
	e.setState("running")
	return e, nil
}

func (e *Engine) launch() error {
	cert, err := syncthing.LoadOrGenerateCertificate(locations.Get(locations.CertFile), locations.Get(locations.KeyFile))
	if err != nil {
		return fmt.Errorf("加载/生成设备证书失败: %w", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	evLogger := events.NewLogger()
	go evLogger.Serve(ctx)
	cfg, err := syncthing.LoadConfigAtStartup(locations.Get(locations.ConfigFile), cert, evLogger, false, false)
	if err != nil {
		cancel()
		return fmt.Errorf("加载同步配置失败: %w", err)
	}
	go cfg.Serve(ctx)
	// GUI/REST 只给本机内核使用：固定监听 127.0.0.1:<随机端口>，随机 API Key，不打开浏览器
	addr := fmt.Sprintf("127.0.0.1:%d", e.port)
	w, err := cfg.Modify(func(c *config.Configuration) {
		// 启动前就应用纯内网默认值（与内核随后通过 REST 写入的 team.LANOptions 完全相同），
		// 保证首次启动也不会连接全局发现/中继等公网服务
		applyLANOptions(&c.Options)
		c.GUI.Enabled = true
		c.GUI.RawAddress = addr
		c.GUI.RawUseTLS = false
		c.GUI.APIKey = e.apiKey
		c.Options.StartBrowser = false
		c.Options.AutoUpgradeIntervalH = 0
	})
	if err != nil {
		cancel()
		return fmt.Errorf("写入同步配置失败: %w", err)
	}
	w.Wait()
	if err := syncthing.TryMigrateDatabase(ctx, dbDeleteRetention); err != nil {
		cancel()
		return fmt.Errorf("迁移数据库失败: %w", err)
	}
	sdb, err := syncthing.OpenDatabase(locations.Get(locations.Database), dbDeleteRetention)
	if err != nil {
		cancel()
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	app, err := syncthing.New(cfg, sdb, evLogger, cert, syncthing.Options{NoUpgrade: true, DBMaintenanceInterval: dbMaintenanceInterval})
	if err != nil {
		cancel()
		return fmt.Errorf("创建同步引擎失败: %w", err)
	}
	if err := app.Start(); err != nil {
		cancel()
		return fmt.Errorf("启动同步引擎失败: %w", err)
	}
	e.mu.Lock()
	e.app = app
	e.cancel = cancel
	e.mu.Unlock()
	return nil
}

func applyLANOptions(o *config.OptionsConfiguration) {
	cur, err := json.Marshal(o)
	if err != nil {
		return
	}
	m := map[string]any{}
	if json.Unmarshal(cur, &m) != nil {
		return
	}
	for k, v := range team.LANOptions {
		m[k] = v
	}
	if b, err := json.Marshal(m); err == nil {
		var n config.OptionsConfiguration
		if json.Unmarshal(b, &n) == nil {
			*o = n
		}
	}
}

// supervise 处理 syncthing 自身请求的重启（如 REST /rest/system/restart）及异常退出（指数退避重启）。
func (e *Engine) supervise() {
	backoff := time.Second
	for {
		e.mu.Lock()
		app, cancel := e.app, e.cancel
		e.mu.Unlock()
		status := app.Wait()
		cancel()
		e.mu.Lock()
		stopped := e.stopped
		e.mu.Unlock()
		if stopped {
			e.setState("stopped")
			close(e.done)
			startOnce.Unlock()
			return
		}
		if status == svcutil.ExitRestart {
			e.setState("starting")
			backoff = time.Second
		} else {
			log.Printf("同步引擎意外退出（%v: %v），%s 后重启", status, app.Error(), backoff)
			e.setState("crashed")
			time.Sleep(backoff)
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
		for {
			if err := e.launch(); err != nil {
				log.Printf("重启同步引擎失败: %v", err)
				time.Sleep(backoff)
				continue
			}
			break
		}
		if e.waitHealthy(60*time.Second) == nil {
			e.setState("running")
		}
	}
}

func (e *Engine) waitHealthy(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	c := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := c.Get(e.BaseURL() + "/rest/noauth/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return errors.New("同步引擎健康检查超时")
}

func (e *Engine) setState(s string) {
	e.mu.Lock()
	e.state = s
	cb := e.onState
	e.mu.Unlock()
	if cb != nil {
		cb(s)
	}
}

func (e *Engine) BaseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", e.port) }
func (e *Engine) APIKey() string  { return e.apiKey }
func (e *Engine) State() string   { e.mu.Lock(); defer e.mu.Unlock(); return e.state }
func (e *Engine) SetOnState(f func(string)) {
	e.mu.Lock()
	e.onState = f
	e.mu.Unlock()
}

// Stop 优雅停止 syncthing（等同 REST shutdown），超时则放弃等待。
func (e *Engine) Stop(timeout time.Duration) error {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return nil
	}
	e.stopped = true
	app := e.app
	e.mu.Unlock()
	if app == nil {
		return nil
	}
	go app.Stop(svcutil.ExitSuccess)
	select {
	case <-e.done:
	case <-time.After(timeout):
		return errors.New("同步引擎停止超时")
	}
	return nil
}

// Package engine 负责启动、监控、重启和关闭捆绑的 syncthing 进程（参考 SyncTrayzor SyncthingProcessRunner）。
package engine

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Config struct {
	Binary  string // syncthing 可执行文件路径
	Home    string // syncthing 配置/数据目录
	Port    int    // GUI/REST 端口，0 表示自动选择
	APIKey  string // 为空则随机生成
	LogOut  io.Writer
	Restart bool // 崩溃后自动重启
}

type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateCrashed  State = "crashed"
)

type Engine struct {
	cfg     Config
	mu      sync.Mutex
	cmd     *exec.Cmd
	state   State
	stopped bool
	done    chan struct{}
	OnState func(State)
}

func GenerateAPIKey() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func New(cfg Config) (*Engine, error) {
	if cfg.Binary == "" || cfg.Home == "" {
		return nil, errors.New("engine: Binary 和 Home 不能为空")
	}
	if cfg.Port == 0 {
		p, err := FreePort()
		if err != nil {
			return nil, err
		}
		cfg.Port = p
	}
	if cfg.APIKey == "" {
		cfg.APIKey = GenerateAPIKey()
	}
	if cfg.LogOut == nil {
		cfg.LogOut = io.Discard
	}
	return &Engine{cfg: cfg, state: StateStopped, done: make(chan struct{})}, nil
}

func (e *Engine) BaseURL() string { return fmt.Sprintf("http://127.0.0.1:%d", e.cfg.Port) }
func (e *Engine) APIKey() string  { return e.cfg.APIKey }
func (e *Engine) State() State    { e.mu.Lock(); defer e.mu.Unlock(); return e.state }

func (e *Engine) setState(s State) {
	e.mu.Lock()
	e.state = s
	cb := e.OnState
	e.mu.Unlock()
	if cb != nil {
		cb(s)
	}
}

// Args 返回启动参数（导出便于测试）。
func (e *Engine) Args() []string {
	return []string{"serve", "--no-browser", "--no-restart",
		"--home=" + e.cfg.Home,
		fmt.Sprintf("--gui-address=127.0.0.1:%d", e.cfg.Port)}
}

// Env 返回启动环境变量。
func (e *Engine) Env() []string {
	return append(os.Environ(),
		"STGUIAPIKEY="+e.cfg.APIKey,
		"STNOUPGRADE=1",
		"STNODEFAULTFOLDER=1",
		"STNOBROWSER=1",
	)
}

func (e *Engine) spawn() (*exec.Cmd, error) {
	if err := os.MkdirAll(e.cfg.Home, 0o700); err != nil {
		return nil, err
	}
	cmd := exec.Command(e.cfg.Binary, e.Args()...)
	cmd.Env = e.Env()
	cmd.Stdout = e.cfg.LogOut
	cmd.Stderr = e.cfg.LogOut
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	attachJob(cmd)
	return cmd, nil
}

// Start 启动 syncthing 并在后台监控；阻塞直到 REST 健康检查通过或超时。
func (e *Engine) Start(ctx context.Context) error {
	cmd, err := e.spawn()
	if err != nil {
		return err
	}
	e.mu.Lock()
	e.cmd = cmd
	e.mu.Unlock()
	e.setState(StateStarting)
	go e.supervise(cmd)
	if err := e.WaitHealthy(ctx, 60*time.Second); err != nil {
		return err
	}
	e.setState(StateRunning)
	return nil
}

func (e *Engine) supervise(cmd *exec.Cmd) {
	backoff := time.Second
	for {
		err := cmd.Wait()
		e.mu.Lock()
		stopped := e.stopped
		e.mu.Unlock()
		if stopped || !e.cfg.Restart {
			e.setState(StateStopped)
			close(e.done)
			return
		}
		log.Printf("syncthing 意外退出: %v，%s 后重启", err, backoff)
		e.setState(StateCrashed)
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
		nc, serr := e.spawn()
		if serr != nil {
			log.Printf("重启失败: %v", serr)
			continue
		}
		e.mu.Lock()
		e.cmd = nc
		e.mu.Unlock()
		cmd = nc
		go func() {
			if e.WaitHealthy(context.Background(), 60*time.Second) == nil {
				e.setState(StateRunning)
			}
		}()
	}
}

func (e *Engine) WaitHealthy(ctx context.Context, timeout time.Duration) error {
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
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
	return errors.New("syncthing 健康检查超时")
}

// Stop 先调用 REST shutdown，超时后强制结束。
func (e *Engine) Stop(timeout time.Duration) error {
	e.mu.Lock()
	e.stopped = true
	cmd := e.cmd
	e.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	req, _ := http.NewRequest("POST", e.BaseURL()+"/rest/system/shutdown", nil)
	req.Header.Set("X-API-Key", e.cfg.APIKey)
	if resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req); err == nil {
		resp.Body.Close()
	}
	select {
	case <-e.done:
		return nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		<-e.done
		return nil
	}
}

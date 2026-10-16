// teamsyncd：团队同步内核。`serve` 启动守护进程，其余子命令通过本地 API 控制正在运行的守护进程。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/lan-sync/teamsync/core/internal/api"
	"github.com/lan-sync/teamsync/core/internal/engine"
	"github.com/lan-sync/teamsync/core/internal/stclient"
	"github.com/lan-sync/teamsync/core/internal/team"
)

const usage = `teamsyncd - 内网团队文件夹同步内核

用法:
  teamsyncd serve [--home DIR] [--syncthing PATH] [--api-port N] [--token T] [--folders-dir DIR] [--listen ADDR] [--exit-on-stdin-close]
  teamsyncd status                     查看状态和本机设备 ID
  teamsyncd folders                    列出团队文件夹
  teamsyncd add-folder <名称> <路径>   创建团队文件夹（自动共享给所有成员）
  teamsyncd devices                    列出成员设备
  teamsyncd add-device <设备ID> [名称] 添加成员
  teamsyncd nearby                     列出局域网发现的设备/待确认设备
  teamsyncd accept <设备ID>            接受待确认设备
  teamsyncd conflicts                  列出冲突文件
  teamsyncd scan <文件夹ID>            立即扫描

全局参数: --home DIR（默认 用户配置目录/TeamSync）
`

func defaultHome() string {
	d, err := os.UserConfigDir()
	if err != nil {
		d = "."
	}
	return filepath.Join(d, "TeamSync")
}

func defaultFoldersDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		h = "."
	}
	return filepath.Join(h, "TeamSync")
}

func defaultSyncthing() string {
	exe, _ := os.Executable()
	dir := filepath.Dir(exe)
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	cands := []string{filepath.Join(dir, "syncthing"+ext)}
	m, _ := filepath.Glob(filepath.Join(dir, "syncthing-*"+ext))
	cands = append(cands, m...)
	for _, c := range cands {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return c
		}
	}
	return "syncthing" + ext
}

type daemonInfo struct {
	Port  int    `json:"port"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

func main() {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Print(usage)
		return
	}
	cmd, args := os.Args[1], os.Args[2:]
	if cmd == "serve" {
		serve(args)
		return
	}
	// --home 可出现在任意位置；也支持环境变量 TEAMSYNC_HOME
	home := os.Getenv("TEAMSYNC_HOME")
	if home == "" {
		home = defaultHome()
	}
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--home" && i+1 < len(args):
			home = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--home="):
			home = strings.TrimPrefix(args[i], "--home=")
		default:
			rest = append(rest, args[i])
		}
	}
	if err := runCLI(cmd, rest, home); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		os.Exit(1)
	}
}

func serve(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	home := fs.String("home", defaultHome(), "数据目录")
	stBin := fs.String("syncthing", defaultSyncthing(), "syncthing 可执行文件")
	apiPort := fs.Int("api-port", 0, "本地 API 端口（0=自动）")
	token := fs.String("token", "", "本地 API token（空=随机）")
	foldersDir := fs.String("folders-dir", defaultFoldersDir(), "自动接收的团队文件夹存放目录")
	listen := fs.String("listen", "", "同步监听地址，如 tcp://0.0.0.0:22001（空=Syncthing 默认 22000）")
	stdinWatch := fs.Bool("exit-on-stdin-close", false, "标准输入关闭时退出（作为 sidecar 时使用）")
	fs.Parse(args)

	if err := os.MkdirAll(*home, 0o700); err != nil {
		log.Fatal(err)
	}
	logf, _ := os.OpenFile(filepath.Join(*home, "syncthing.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	eng, err := engine.New(engine.Config{Binary: *stBin, Home: filepath.Join(*home, "st"), LogOut: logf, Restart: true})
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	log.Printf("启动 syncthing: %s (REST %s)", *stBin, eng.BaseURL())
	if err := eng.Start(ctx); err != nil {
		log.Fatalf("启动 syncthing 失败: %v", err)
	}
	c := stclient.New(eng.BaseURL(), eng.APIKey())
	ts := &team.Service{C: c}
	if err := ts.ApplyLANDefaults(ctx); err != nil {
		log.Printf("应用内网默认配置失败: %v", err)
	}
	if *listen != "" {
		if err := c.PatchOptions(ctx, map[string]any{"listenAddresses": strings.Split(*listen, ",")}); err != nil {
			log.Printf("设置监听地址失败: %v", err)
		}
	}
	if err := ts.ApplyFolderDefaults(ctx, *foldersDir); err != nil {
		log.Printf("设置文件夹默认值失败: %v", err)
	}

	if *token == "" {
		*token = engine.GenerateAPIKey()
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *apiPort))
	if err != nil {
		log.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	srv := api.New(c, *token, func() string { return string(eng.State()) })
	eng.OnState = func(s engine.State) {
		srv.Publish(api.UIEvent{Type: "EngineState", Time: time.Now(), Data: map[string]string{"state": string(s)}})
	}
	go srv.RunEventBridge(ctx)
	hs := &http.Server{Handler: srv.Handler()}
	go hs.Serve(ln)

	info, _ := json.Marshal(daemonInfo{Port: port, Token: *token, PID: os.Getpid()})
	infoPath := filepath.Join(*home, "daemon.json")
	_ = os.WriteFile(infoPath, info, 0o600)
	fmt.Printf("TEAMSYNCD_READY %d\n", port)
	log.Printf("本地 API: http://127.0.0.1:%d", port)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	if *stdinWatch {
		go func() { io.Copy(io.Discard, os.Stdin); sig <- syscall.SIGTERM }()
	}
	<-sig
	log.Print("正在关闭...")
	cancel()
	sctx, scancel := context.WithTimeout(context.Background(), 3*time.Second)
	hs.Shutdown(sctx)
	scancel()
	eng.Stop(10 * time.Second)
	os.Remove(infoPath)
}

func runCLI(cmd string, args []string, home string) error {
	b, err := os.ReadFile(filepath.Join(home, "daemon.json"))
	if err != nil {
		return fmt.Errorf("内核未运行（找不到 %s/daemon.json），请先执行 teamsyncd serve", home)
	}
	var info daemonInfo
	if err := json.Unmarshal(b, &info); err != nil {
		return err
	}
	call := func(method, path string, body any) error {
		var rd io.Reader
		if body != nil {
			j, _ := json.Marshal(body)
			rd = bytes.NewReader(j)
		}
		req, _ := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", info.Port, path), rd)
		req.Header.Set("Authorization", "Bearer "+info.Token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		var pretty bytes.Buffer
		if json.Indent(&pretty, data, "", "  ") == nil {
			data = pretty.Bytes()
		}
		fmt.Println(strings.TrimSpace(string(data)))
		if resp.StatusCode >= 300 {
			return fmt.Errorf("HTTP %d", resp.StatusCode)
		}
		return nil
	}
	need := func(n int) error {
		if len(args) < n {
			return fmt.Errorf("参数不足\n\n%s", usage)
		}
		return nil
	}
	switch cmd {
	case "status":
		return call("GET", "/api/status", nil)
	case "folders":
		return call("GET", "/api/folders", nil)
	case "add-folder":
		if err := need(2); err != nil {
			return err
		}
		p, _ := filepath.Abs(args[1])
		return call("POST", "/api/folders", map[string]string{"label": args[0], "path": p})
	case "devices":
		return call("GET", "/api/devices", nil)
	case "add-device":
		if err := need(1); err != nil {
			return err
		}
		name := ""
		if len(args) > 1 {
			name = args[1]
		}
		return call("POST", "/api/devices", map[string]string{"deviceID": args[0], "name": name})
	case "nearby":
		return call("GET", "/api/nearby", nil)
	case "accept":
		if err := need(1); err != nil {
			return err
		}
		return call("POST", "/api/pending/devices/"+args[0]+"/accept", nil)
	case "conflicts":
		return call("GET", "/api/conflicts", nil)
	case "scan":
		if err := need(1); err != nil {
			return err
		}
		return call("POST", "/api/folders/"+args[0]+"/scan", nil)
	}
	return fmt.Errorf("未知命令 %q\n\n%s", cmd, usage)
}

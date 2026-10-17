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
  teamsyncd add-folder <名称> <路径> [设备ID...]  创建团队文件夹并共享给指定成员（不指定=全部成员）
  teamsyncd share <文件夹ID> [设备ID...]          设置文件夹共享成员
  teamsyncd remove-folder <文件夹ID> [--delete-files] [--permanent]  仅在本机移除文件夹（可同时删除本地文件，默认进回收站）
  teamsyncd reinvite <文件夹ID> <设备ID>          重新邀请成员
  teamsyncd invites                               查看收到的文件夹邀请
  teamsyncd accept-invite <文件夹ID> <邀请人设备ID> <本地路径>
  teamsyncd ignore-invite <文件夹ID> <邀请人设备ID>
  teamsyncd settings [默认存放目录]               查看/修改接收邀请的默认目录
  teamsyncd devices                    列出成员设备
  teamsyncd add-device <设备ID> [名称] 添加成员
  teamsyncd nearby                     列出局域网发现的设备/待确认设备
  teamsyncd accept <设备ID>            接受待确认设备
  teamsyncd conflicts                  列出冲突文件
  teamsyncd scan <文件夹ID>            立即扫描
  teamsyncd rename-device <设备ID> <备注名>  修改成员备注名（仅本机）
  teamsyncd my-name [新名称]            查看/修改"我的名称"（其他成员会看到）
  teamsyncd rename-device <设备ID> --announced  清除备注，改用对方自己设置的名称
  teamsyncd repair <文件夹ID> [--force]  重建丢失的 .stfolder 标记并重新扫描（空目录需 --force）
  teamsyncd relocate <文件夹ID> <新路径>  文件夹路径丢失时重新指定位置

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
	foldersDir := fs.String("folders-dir", defaultFoldersDir(), "接收邀请时的默认存放目录（首次运行时写入设置，之后以设置为准）")
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
	if err := ts.DisableAutoAccept(ctx); err != nil {
		log.Printf("关闭自动接收失败: %v", err)
	}
	store := team.OpenStore(filepath.Join(*home, "settings.json"), *foldersDir)
	if err := ts.MigrateRemarks(ctx, store); err != nil {
		log.Printf("迁移成员备注: %v", err)
	}
	if err := ts.ApplyFolderDefaults(ctx, store.Get().DefaultBaseDir); err != nil {
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
	srv := api.New(c, *token, func() string { return string(eng.State()) }, store)
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
	// 先停 syncthing，再关闭本地 API：API 端口关闭即表示 syncthing 已退出（安装更新时据此判断）
	eng.Stop(10 * time.Second)
	sctx, scancel := context.WithTimeout(context.Background(), 3*time.Second)
	hs.Shutdown(sctx)
	scancel()
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
		body := map[string]any{"label": args[0], "path": p}
		if len(args) > 2 {
			body["members"] = args[2:]
		}
		return call("POST", "/api/folders", body)
	case "share":
		if err := need(1); err != nil {
			return err
		}
		return call("PUT", "/api/folders/"+args[0]+"/members", map[string]any{"members": append([]string{}, args[1:]...)})
	case "remove-folder":
		var id string
		q := []string{}
		for _, a := range args {
			switch a {
			case "--delete-files":
				q = append(q, "deleteFiles=1")
			case "--permanent":
				q = append(q, "permanent=1")
			default:
				id = a
			}
		}
		if id == "" {
			return fmt.Errorf("缺少文件夹 ID\n\n%s", usage)
		}
		path := "/api/folders/" + id
		if len(q) > 0 {
			path += "?" + strings.Join(q, "&")
		}
		return call("DELETE", path, nil)
	case "reinvite":
		if err := need(2); err != nil {
			return err
		}
		return call("POST", "/api/folders/"+args[0]+"/reinvite", map[string]string{"deviceID": args[1]})
	case "invites":
		return call("GET", "/api/invitations", nil)
	case "accept-invite", "ignore-invite":
		if err := need(2); err != nil {
			return err
		}
		inv, err := findInvite(info, args[0], args[1])
		if err != nil {
			return err
		}
		if cmd == "ignore-invite" {
			return call("POST", "/api/invitations/ignore", map[string]string{"folderID": inv.FolderID, "deviceID": inv.DeviceID, "offerTime": inv.OfferTime})
		}
		p := inv.SuggestPath
		if len(args) > 2 {
			p, _ = filepath.Abs(args[2])
		}
		return call("POST", "/api/invitations/accept", map[string]string{"folderID": inv.FolderID, "deviceID": inv.DeviceID, "label": inv.Label, "path": p})
	case "settings":
		if len(args) > 0 {
			p, _ := filepath.Abs(args[0])
			return call("PUT", "/api/settings", map[string]string{"defaultBaseDir": p})
		}
		return call("GET", "/api/settings", nil)
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
	case "rename-device":
		if err := need(2); err != nil {
			return err
		}
		if args[1] == "--announced" {
			return call("PATCH", "/api/devices/"+args[0], map[string]bool{"useAnnounced": true})
		}
		return call("PATCH", "/api/devices/"+args[0], map[string]string{"name": strings.Join(args[1:], " ")})
	case "my-name":
		if len(args) == 0 {
			return call("GET", "/api/me", nil)
		}
		return call("PUT", "/api/me", map[string]string{"name": strings.Join(args, " ")})
	case "repair":
		if err := need(1); err != nil {
			return err
		}
		return call("POST", "/api/folders/"+args[0]+"/repair", map[string]bool{"force": len(args) > 1 && args[1] == "--force"})
	case "relocate":
		if err := need(2); err != nil {
			return err
		}
		return call("POST", "/api/folders/"+args[0]+"/relocate", map[string]string{"path": args[1]})
	}
	return fmt.Errorf("未知命令 %q\n\n%s", cmd, usage)
}

func findInvite(info daemonInfo, folderID, deviceID string) (team.Invitation, error) {
	req, _ := http.NewRequest("GET", fmt.Sprintf("http://127.0.0.1:%d/api/invitations", info.Port), nil)
	req.Header.Set("Authorization", "Bearer "+info.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return team.Invitation{}, err
	}
	defer resp.Body.Close()
	var list []team.Invitation
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		return team.Invitation{}, err
	}
	for _, i := range list {
		if i.FolderID == folderID && (deviceID == "" || i.DeviceID == deviceID) {
			return i, nil
		}
	}
	return team.Invitation{}, fmt.Errorf("没有找到文件夹 %s 的邀请", folderID)
}

// teamsync-mobile-node：在电脑上运行与 Android/iOS App 完全相同的移动端内核（进程内嵌入 syncthing），
// 只用于协议互通测试（scripts/e2e-mobile-desktop.sh）。它写出与桌面 teamsyncd 相同的 daemon.json，
// 因此可以直接用桌面 CLI `teamsyncd --home <dir> ...` 操作它。
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/lan-sync/teamsync/core/mobile/mobilecore"
)

func main() {
	home := flag.String("home", "", "数据目录")
	folders := flag.String("folders-dir", "", "默认存放目录")
	listen := flag.String("listen", "", "同步监听地址")
	apiPort := flag.Int("api-port", 0, "本地 API 端口")
	noTrash := flag.Bool("no-trash", true, "模拟手机：没有回收站")
	flag.Parse()
	if *home == "" || *folders == "" {
		log.Fatal("需要 --home 和 --folders-dir")
	}
	n, err := mobilecore.Start(mobilecore.Config{Home: *home, FoldersDir: *folders, Listen: *listen, APIPort: *apiPort, NoTrash: *noTrash})
	if err != nil {
		log.Fatal(err)
	}
	info, _ := json.Marshal(map[string]any{"port": n.Port(), "token": n.Token(), "pid": os.Getpid()})
	infoPath := filepath.Join(*home, "daemon.json")
	_ = os.WriteFile(infoPath, info, 0o600)
	fmt.Printf("TEAMSYNCD_READY %d\n", n.Port())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	mobilecore.Stop()
	os.Remove(infoPath)
}

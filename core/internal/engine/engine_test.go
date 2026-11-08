package engine

import (
	"fmt"
	"strings"
	"testing"
)

func TestNewDefaults(t *testing.T) {
	e, err := New(Config{Binary: "syncthing", Home: "/tmp/h"})
	if err != nil {
		t.Fatal(err)
	}
	if len(e.APIKey()) != 48 || e.cfg.Port == 0 {
		t.Fatal("key/port not generated")
	}
	args := strings.Join(e.Args(), " ")
	for _, want := range []string{"serve", "--no-browser", "--no-restart", "--home=/tmp/h", "--gui-address=127.0.0.1:"} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing %s in %s", want, args)
		}
	}
	env := strings.Join(e.Env(), "\n")
	for _, want := range []string{"STGUIAPIKEY=" + e.APIKey(), "STNOUPGRADE=1"} {
		if !strings.Contains(env, want) {
			t.Fatalf("missing env %s", want)
		}
	}
	if _, err := New(Config{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestFreePort(t *testing.T) {
	p, err := FreePort()
	if err != nil || p <= 0 {
		t.Fatal(err)
	}
}

func TestStaleTargets(t *testing.T) {
	bin := "/Applications/TeamSync.app/Contents/MacOS/syncthing"
	home := "/Users/a/Library/Application Support/TeamSync/st"
	args := " serve --no-browser --no-restart --home=" + home + " --gui-address=127.0.0.1:1234"
	ps := strings.Join([]string{
		"  100     1 " + bin + args,                              // 被收养的监控进程（记录的 PID）
		"  104   100 " + bin + args,                              // 其工作进程
		"  200     1 " + bin + args,                              // 被收养的另一个旧进程（PID 文件丢失时）
		"  300   250 " + bin + args,                              // 仍有父进程且与记录无关：不处理
		"  400     1 " + bin + " serve --home=/tmp/other/st",     // 其他数据目录
		"  500     1 /usr/bin/vim " + home,                       // 其他程序
		"  600     1 " + bin + " serve --home=" + home + "2 --x", // 数据目录只是前缀相同
		"  700     1 " + bin + " serve --home=" + home,           // 参数在末尾
		"garbage",
	}, "\n")
	got := fmt.Sprint(staleTargets(ps, bin, home, 100, 999))
	if got != "[100 104 200 700]" {
		t.Fatal(got)
	}
	if got := fmt.Sprint(staleTargets(ps, bin, home, 0, 200)); got != "[100 700]" {
		t.Fatal(got)
	}
}

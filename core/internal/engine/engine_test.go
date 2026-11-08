package engine

import (
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

func TestIsOrphanOf(t *testing.T) {
	bin := "/Applications/TeamSync.app/Contents/MacOS/syncthing"
	cases := map[string]bool{
		"    1 /Applications/TeamSync.app/Contents/MacOS/syncthing\n": true,
		"  1 /tmp/other/syncthing":                                    true,  // 同名即可（数据目录由 PID 文件限定）
		"  523 /Applications/TeamSync.app/Contents/MacOS/syncthing":   false, // 仍有父进程：不是孤儿
		"    1 /usr/bin/vim":                                          false,
		"":                                                            false,
		"x y":                                                         false,
	}
	for in, want := range cases {
		if got := isOrphanOf(in, bin); got != want {
			t.Errorf("%q => %v, want %v", in, got, want)
		}
	}
}

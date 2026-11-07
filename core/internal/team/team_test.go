package team

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestNewFolderID(t *testing.T) {
	re := regexp.MustCompile(`^[a-z2-9]{5}-[a-z2-9]{5}$`)
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		id := NewFolderID()
		if !re.MatchString(id) || seen[id] {
			t.Fatalf("bad/dup id %s", id)
		}
		seen[id] = true
	}
}

func TestNewTeamFolderDefaults(t *testing.T) {
	f := NewTeamFolder("设计文档", "/tmp/x", []string{"A", "B", "A", " "})
	if !f.FSWatcherEnabled || f.Versioning.Type != "simple" || f.Type != "sendreceive" {
		t.Fatalf("defaults wrong: %+v", f)
	}
	if len(f.Devices) != 2 {
		t.Fatalf("devices %v", f.Devices)
	}
}

func TestLANOptions(t *testing.T) {
	if LANOptions["globalAnnounceEnabled"] != false || LANOptions["relaysEnabled"] != false || LANOptions["localAnnounceEnabled"] != true {
		t.Fatal("LAN defaults wrong")
	}
}

func TestWriteIgnores(t *testing.T) {
	d := t.TempDir()
	if err := WriteIgnores(d); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(d, ".stignore"), []byte("custom"), 0o644)
	WriteIgnores(d)
	if b, _ := os.ReadFile(filepath.Join(d, ".stignore")); string(b) != "custom" {
		t.Fatal("should not overwrite")
	}
}

func TestNormalizeAddress(t *testing.T) {
	ok := map[string]string{
		"192.168.1.5":           "tcp://192.168.1.5:22000",
		" 192.168.1.5:22001 ":   "tcp://192.168.1.5:22001",
		"tcp://10.0.0.2:22000":  "tcp://10.0.0.2:22000",
		"quic://10.0.0.2:22000": "quic://10.0.0.2:22000",
		"laptop.local":          "tcp://laptop.local:22000",
		"[fd00::1]:22000":       "tcp://[fd00::1]:22000",
	}
	for in, want := range ok {
		if got, err := NormalizeAddress(in); err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "http://x/y", "1.2.3.4:0", "1.2.3.4:70000", "a b", "user@host"} {
		if _, err := NormalizeAddress(bad); err == nil {
			t.Errorf("%q 应被拒绝", bad)
		}
	}
}

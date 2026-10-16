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

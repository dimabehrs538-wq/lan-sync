package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func filesServer(t *testing.T, root, ftype string) (*httptest.Server, *[]string) {
	t.Helper()
	var scans []string
	st := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/rest/config/folders/f1":
			json.NewEncoder(w).Encode(map[string]any{"id": "f1", "path": root, "type": ftype})
		case r.URL.Path == "/rest/db/scan":
			scans = append(scans, r.URL.Query().Get("sub"))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(st.Close)
	srv := httptest.NewServer(New(stclient.New(st.URL, "k"), "tok", func() string { return "running" }, nil).Handler())
	t.Cleanup(srv.Close)
	return srv, &scans
}

func call(t *testing.T, method, u string, body io.Reader) (int, []byte) {
	t.Helper()
	req, _ := http.NewRequest(method, u, body)
	req.Header.Set("Authorization", "Bearer tok")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestFilesListUploadDownload(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, ".stfolder"), 0o755)
	os.MkdirAll(filepath.Join(root, ".stversions"), 0o755)
	os.MkdirAll(filepath.Join(root, "设计"), 0o755)
	os.WriteFile(filepath.Join(root, "接口.md"), []byte("v1"), 0o644)
	os.WriteFile(filepath.Join(root, ".teamsync.json"), []byte("{}"), 0o644)
	srv, scans := filesServer(t, root, "sendreceive")

	code, b := call(t, "GET", srv.URL+"/api/folders/f1/files", nil)
	if code != 200 {
		t.Fatal(code, string(b))
	}
	var l struct{ Entries []FileEntry }
	json.Unmarshal(b, &l)
	if len(l.Entries) != 2 || !l.Entries[0].Dir || l.Entries[0].Name != "设计" || l.Entries[1].Name != "接口.md" || l.Entries[1].Size != 2 {
		t.Fatalf("内部文件应隐藏、目录在前: %+v", l.Entries)
	}

	// 上传：不覆盖同名文件，自动改名
	code, b = call(t, "POST", srv.URL+"/api/folders/f1/files?name="+url.QueryEscape("接口.md"), strings.NewReader("v2"))
	if code != 200 || !strings.Contains(string(b), `"name":"接口 (2).md"`) {
		t.Fatal(code, string(b))
	}
	if got, _ := os.ReadFile(filepath.Join(root, "接口.md")); string(got) != "v1" {
		t.Fatal("原文件被覆盖")
	}
	// 上传到子目录（自动创建）、覆盖
	code, b = call(t, "POST", srv.URL+"/api/folders/f1/files?dir="+url.QueryEscape("设计/新建")+"&name=a.txt", strings.NewReader("A"))
	if code != 200 {
		t.Fatal(code, string(b))
	}
	code, _ = call(t, "POST", srv.URL+"/api/folders/f1/files?dir="+url.QueryEscape("设计/新建")+"&name=a.txt&overwrite=1", strings.NewReader("B"))
	if got, _ := os.ReadFile(filepath.Join(root, "设计", "新建", "a.txt")); code != 200 || string(got) != "B" {
		t.Fatal("覆盖失败", code, string(got))
	}
	ents, _ := os.ReadDir(filepath.Join(root, "设计", "新建"))
	if len(ents) != 1 {
		t.Fatalf("不应残留临时文件: %v", ents)
	}

	code, b = call(t, "GET", srv.URL+"/api/folders/f1/file?path="+url.QueryEscape("设计/新建/a.txt"), nil)
	if code != 200 || string(b) != "B" {
		t.Fatal(code, string(b))
	}

	// 越界与内部文件
	for _, bad := range []string{"../x", "/etc/passwd", ".stfolder", "设计/../../x", ".stversions/a~1.md"} {
		if code, _ := call(t, "GET", srv.URL+"/api/folders/f1/file?path="+url.QueryEscape(bad), nil); code != 400 {
			t.Errorf("%s 应被拒绝，得到 %d", bad, code)
		}
	}
	for _, bad := range []string{".teamsync.json", ".stignore", "", ".."} {
		if code, _ := call(t, "POST", srv.URL+"/api/folders/f1/files?name="+url.QueryEscape(bad), strings.NewReader("x")); code != 400 {
			t.Errorf("上传 %q 应被拒绝，得到 %d", bad, code)
		}
	}
	_ = scans
}

func TestFilesSymlinkEscape(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(outside, "key.pem"), []byte("secret"), 0o600)
	if err := os.Symlink(filepath.Join(outside, "key.pem"), filepath.Join(root, "link.pem")); err != nil {
		t.Skip("无法创建符号链接", err)
	}
	os.Symlink(outside, filepath.Join(root, "out"))
	srv, _ := filesServer(t, root, "sendreceive")
	if code, b := call(t, "GET", srv.URL+"/api/folders/f1/file?path=link.pem", nil); code == 200 {
		t.Fatalf("符号链接不能读出文件夹之外的文件: %s", b)
	}
	if code, _ := call(t, "POST", srv.URL+"/api/folders/f1/files?dir=out&name=x.txt", strings.NewReader("x")); code == 200 {
		t.Fatal("符号链接不能把写入引到文件夹之外")
	}
	if _, err := os.Stat(filepath.Join(outside, "x.txt")); err == nil {
		t.Fatal("文件被写到了文件夹之外")
	}
}

func TestFilesReadonlyRejectsUpload(t *testing.T) {
	srv, _ := filesServer(t, t.TempDir(), "receiveonly")
	if code, b := call(t, "POST", srv.URL+"/api/folders/f1/files?name=a.txt", strings.NewReader("x")); code != 403 || !strings.Contains(string(b), "只读") {
		t.Fatal(code, string(b))
	}
}

func TestLANAddresses(t *testing.T) {
	var st stclient.SystemStatus
	json.Unmarshal([]byte(`{"myID":"X","connectionServiceStatus":{
	  "tcp://0.0.0.0:22000":{"lanAddresses":["tcp://0.0.0.0:22000","tcp://192.168.1.5:22000","tcp://127.0.0.1:22000","tcp://[fe80::1%25en0]:22000","tcp://192.168.1.5:22000"]},
	  "quic://0.0.0.0:22000":{"lanAddresses":["quic://192.168.1.5:22000"]}}}`), &st)
	got := lanAddresses(st)
	if len(got) != 1 || got[0] != "tcp://192.168.1.5:22000" {
		t.Fatalf("%v", got)
	}
}

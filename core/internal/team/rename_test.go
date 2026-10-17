package team

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lan-sync/teamsync/core/internal/stclient"
)

func TestNormalizeName(t *testing.T) {
	if n, err := NormalizeName("  张三 \t 设计  "); err != nil || n != "张三 设计" {
		t.Fatalf("%q %v", n, err)
	}
	for _, bad := range []string{"", "   ", strings.Repeat("长", MaxNameLen+1)} {
		if _, err := NormalizeName(bad); err == nil {
			t.Errorf("should reject %q", bad)
		}
	}
	if _, err := NormalizeName(strings.Repeat("长", MaxNameLen)); err != nil {
		t.Error(err)
	}
}

func TestRenameDevice(t *testing.T) {
	var patched map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/rest/config/devices":
			w.Write([]byte(`[{"deviceID":"PEER","name":"old"}]`))
		case r.Method == "PATCH" && r.URL.Path == "/rest/config/devices/PEER":
			json.NewDecoder(r.Body).Decode(&patched)
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	s := &Service{C: stclient.New(srv.URL, "k")}
	n, err := s.RenameDevice(context.Background(), "PEER", "  新名字 ")
	if err != nil || n != "新名字" || patched["name"] != "新名字" || len(patched) != 1 {
		t.Fatalf("%q %v %v", n, err, patched)
	}
	if _, err := s.RenameDevice(context.Background(), "NOPE", "x"); !errors.Is(err, ErrDeviceNotFound) {
		t.Fatal(err)
	}
	patched = nil
	if _, err := s.RenameDevice(context.Background(), "PEER", " "); err == nil || patched != nil {
		t.Fatal("empty name must not patch")
	}
}

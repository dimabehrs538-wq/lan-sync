package stclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientAuthAndEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/rest/system/status":
			w.Write([]byte(`{"myID":"ME"}`))
		case "/rest/events":
			if !strings.Contains(r.URL.Query().Get("events"), "RemoteChangeDetected") || r.URL.Query().Get("since") != "5" {
				w.WriteHeader(400)
				return
			}
			w.Write([]byte(`[{"id":6,"type":"RemoteChangeDetected","data":{}}]`))
		}
	}))
	defer srv.Close()
	c := New(srv.URL, "k")
	st, err := c.Status(context.Background())
	if err != nil || st.MyID != "ME" {
		t.Fatal(err, st)
	}
	ev, err := c.Events(context.Background(), 5, time.Second)
	if err != nil || len(ev) != 1 || ev[0].ID != 6 {
		t.Fatal(err, ev)
	}
	bad := New(srv.URL, "x")
	if _, err := bad.Status(context.Background()); err == nil {
		t.Fatal("expected auth error")
	}
}

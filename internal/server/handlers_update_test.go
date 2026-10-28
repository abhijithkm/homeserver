package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/abhijithkm/homeserver/internal/update"
)

func TestUpdateEndpoint(t *testing.T) {
	var hits atomic.Int32
	var gh *httptest.Server
	gh = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprintf(w, `[{"tag_name":"v1.0.0","prerelease":false,"draft":false,"html_url":"%s/r","assets":[
			{"name":"HomeServer-Setup-1.0.0.exe","browser_download_url":"%s/a"},
			{"name":"SHA256SUMS.txt","browser_download_url":"%s/b"}]}]`, gh.URL, gh.URL, gh.URL)
	}))
	defer gh.Close()

	e := newEnv(t)
	e.setup()
	e.srv.cfg.Version = "1.0.0-rc1"
	e.srv.cfg.Updater = &update.Client{HTTP: gh.Client(), APIBase: gh.URL, TrustAsset: func(string) bool { return true }}

	type resp struct {
		Enabled bool
		Error   string
		Info    struct {
			Available bool
			Latest    string
		}
	}
	get := func(q string) resp {
		code, b := e.do("GET", "/api/admin/update"+q, nil, nil)
		if code != 200 {
			t.Fatalf("update: %d %s", code, b)
		}
		var r resp
		_ = json.Unmarshal(b, &r)
		return r
	}

	if r := get(""); !r.Enabled || !r.Info.Available || r.Info.Latest != "1.0.0" {
		t.Fatalf("expected 1.0.0 offered to an rc1 install: %+v", r)
	}
	get("")
	get("?refresh=1") // within the refresh floor: still served from cache
	if n := hits.Load(); n != 1 {
		t.Fatalf("GitHub queried %d times, want 1 (cached)", n)
	}

	set := e.srv.settings()
	set.UpdateCheck = false
	if code, b := e.do("PUT", "/api/admin/settings", set, nil); code != 200 {
		t.Fatalf("save settings: %d %s", code, b)
	}
	if r := get(""); r.Enabled {
		t.Fatalf("update check disabled in settings must not report: %+v", r)
	}
	if n := hits.Load(); n != 1 {
		t.Fatal("a disabled check must not contact GitHub")
	}
}

func TestUpdateEndpointRequiresAdmin(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.do("GET", "/api/admin/update", nil, nil); code != http.StatusUnauthorized {
		t.Fatalf("anonymous: %d", code)
	}
}

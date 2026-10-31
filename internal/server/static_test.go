package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/abhijithkm/homeserver/internal/paths"
	"github.com/abhijithkm/homeserver/internal/store"
)

// The app manifest and service worker are what let a phone or PC install the web page as an app.
func TestStaticServesManifestAndServiceWorker(t *testing.T) {
	d := paths.Resolve(t.TempDir())
	st, _, err := store.Open(d.DB)
	if err != nil {
		t.Fatal(err)
	}
	web := fstest.MapFS{
		"index.html":           {Data: []byte("<!doctype html><title>x</title>")},
		"manifest.webmanifest": {Data: []byte(`{"name":"HomeServer"}`)},
		"sw.js":                {Data: []byte("self.addEventListener('fetch',()=>{})")},
		"icon-192.png":         {Data: []byte("png")},
	}
	srv, err := New(Config{Store: st, Dirs: d, Version: "test", Web: web})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); st.Close() })

	get := func(p string) (*http.Response, string) {
		t.Helper()
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	resp, body := get("/manifest.webmanifest")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "application/manifest+json" || !strings.Contains(body, "HomeServer") {
		t.Fatalf("manifest: status %d, type %q, body %q", resp.StatusCode, resp.Header.Get("Content-Type"), body)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("manifest must be revalidated, Cache-Control = %q", got)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("security headers missing on the manifest")
	}

	resp, _ = get("/sw.js")
	if ct := resp.Header.Get("Content-Type"); resp.StatusCode != 200 || !strings.Contains(ct, "javascript") {
		t.Fatalf("service worker: status %d, type %q (browsers refuse a worker that is not JavaScript)", resp.StatusCode, ct)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("service worker must be revalidated, Cache-Control = %q", got)
	}

	resp, _ = get("/icon-192.png")
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("icon: status %d, type %q", resp.StatusCode, resp.Header.Get("Content-Type"))
	}

	// An unknown path still falls back to the app shell, so deep links keep working.
	resp, body = get("/nope/page")
	if resp.StatusCode != 200 || !strings.Contains(body, "<title>x</title>") {
		t.Fatalf("fallback: status %d, body %q", resp.StatusCode, body)
	}
}

package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	order := []string{"0.9.0", "1.0.0-rc1", "1.0.0-rc2", "1.0.0-rc10", "1.0.0", "1.0.1", "1.1.0", "2.0.0"}
	for i := range order {
		for j := range order {
			a, _ := parseVersion(order[i])
			b, _ := parseVersion(order[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := compare(a, b); got != want {
				t.Errorf("compare(%s,%s)=%d want %d", order[i], order[j], got, want)
			}
		}
	}
	for _, bad := range []string{"dev", "", "1.0", "1.x.0", "v1.0.0.0"} {
		if _, ok := parseVersion(bad); ok {
			t.Errorf("%q should not parse", bad)
		}
	}
}

func sum(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

// fakeGitHub serves a release list and its assets.
func fakeGitHub(t *testing.T, installer []byte, sumLine string) *Client {
	t.Helper()
	var srv *httptest.Server
	mux := http.NewServeMux()
	rel := func(tag string, pre, draft, complete bool) string {
		assets := "[]"
		if complete {
			v := strings.TrimPrefix(tag, "v")
			assets = fmt.Sprintf(`[{"name":"HomeServer-Setup-%s.exe","browser_download_url":"%s/dl/setup-%s.exe"},{"name":"SHA256SUMS-%s.txt","browser_download_url":"%s/dl/sums-%s.txt"}]`,
				v, srv.URL, v, v, srv.URL, v)
		}
		return fmt.Sprintf(`{"tag_name":"%s","prerelease":%v,"draft":%v,"html_url":"%s/rel/%s","assets":%s}`, tag, pre, draft, srv.URL, tag, assets)
	}
	mux.HandleFunc("/repos/"+Repo+"/releases", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "[%s,%s,%s,%s,%s]",
			rel("v1.2.0", false, true, true),     // draft: ignored
			rel("v1.1.0", false, false, false),   // no assets: ignored
			rel("v1.0.1-rc1", true, false, true), // pre-release
			rel("v1.0.0", false, false, true),
			rel("v0.9.0", false, false, true))
	})
	mux.HandleFunc("/dl/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/dl/sums-") {
			fmt.Fprint(w, sumLine)
			return
		}
		_, _ = w.Write(installer)
	})
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &Client{HTTP: srv.Client(), APIBase: srv.URL, TrustAsset: func(string) bool { return true }}
}

func TestCheckSelectsNewestApplicable(t *testing.T) {
	c := fakeGitHub(t, nil, "")
	ctx := context.Background()

	info, err := c.Check(ctx, "0.9.0", false)
	if err != nil || !info.Available || info.Latest != "1.0.0" || info.Prerelease {
		t.Fatalf("stable user: %+v %v", info, err)
	}
	info, _ = c.Check(ctx, "0.9.0", true)
	if info.Latest != "1.0.1-rc1" || !info.Prerelease {
		t.Fatalf("opt-in to pre-releases: %+v", info)
	}
	info, _ = c.Check(ctx, "1.0.0-rc1", false) // a pre-release build follows pre-releases
	if info.Latest != "1.0.1-rc1" {
		t.Fatalf("rc user: %+v", info)
	}
	info, _ = c.Check(ctx, "1.0.0", false)
	if info.Available {
		t.Fatalf("already current: %+v", info)
	}
	if _, err := c.Check(ctx, "dev", false); err != ErrDevBuild {
		t.Fatalf("dev build: %v", err)
	}
}

func TestCheckRejectsUntrustedAssetLinks(t *testing.T) {
	c := fakeGitHub(t, nil, "")
	c.TrustAsset = nil // strict default: the test server is not github.com
	info, err := c.Check(context.Background(), "0.9.0", false)
	if err != nil || info.Available {
		t.Fatalf("assets outside github.com/%s must not be offered: %+v %v", Repo, info, err)
	}
	for raw, ok := range map[string]bool{
		"https://github.com/" + Repo + "/releases/download/v1/x.exe":          true,
		"http://github.com/" + Repo + "/releases/download/v1/x.exe":           false,
		"https://github.com/evil/homeserver/releases/download/v1/x":           false,
		"https://evil.example/" + Repo + "/releases/download/v1/x":            false,
		"https://github.com.evil.example/" + Repo + "/releases/download/v1/x": false,
	} {
		if releaseAsset(raw) != ok {
			t.Errorf("releaseAsset(%q) != %v", raw, ok)
		}
	}
}

func TestDownloadVerifiesChecksum(t *testing.T) {
	payload := []byte("pretend installer bytes")
	name := "HomeServer-Setup-1.0.0.exe"
	good := fakeGitHub(t, payload, sum(payload)+"  "+name+"\n")
	info, _ := good.Check(context.Background(), "0.9.0", false)
	p, err := good.Download(context.Background(), info, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != string(payload) || filepath.Base(p) != name {
		t.Fatalf("unexpected file %s", p)
	}

	tampered := fakeGitHub(t, []byte("different bytes"), sum(payload)+"  "+name+"\n")
	info, _ = tampered.Check(context.Background(), "0.9.0", false)
	dir := t.TempDir()
	if _, err := tampered.Download(context.Background(), info, dir); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("expected checksum mismatch, got %v", err)
	}
	if es, _ := os.ReadDir(dir); len(es) != 0 {
		t.Fatal("a failed download must leave no file behind")
	}

	missing := fakeGitHub(t, payload, sum(payload)+"  other.exe\n")
	info, _ = missing.Check(context.Background(), "0.9.0", false)
	if _, err := missing.Download(context.Background(), info, t.TempDir()); err == nil {
		t.Fatal("a checksum file that does not list the installer must be refused")
	}
}

func TestFindSum(t *testing.T) {
	h := strings.Repeat("ab", 32)
	if got, err := findSum(strings.ToUpper(h)+" *a.exe\n"+h+"  b.exe", "a.exe"); err != nil || got != h {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := findSum("zz  a.exe", "a.exe"); err == nil {
		t.Fatal("non-hex checksum accepted")
	}
}

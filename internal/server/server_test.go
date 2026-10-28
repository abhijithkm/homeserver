package server

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/abhijithkm/homeserver/internal/paths"
	"github.com/abhijithkm/homeserver/internal/store"
)

type env struct {
	t    *testing.T
	srv  *Server
	st   *store.Store
	ts   *httptest.Server
	c    *http.Client
	csrf string
	data string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	return startEnv(t, t.TempDir())
}

func startEnv(t *testing.T, data string) *env {
	t.Helper()
	d := paths.Resolve(data)
	st, _, err := store.Open(d.DB)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(Config{Store: st, Dirs: d, Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	jar, _ := cookiejar.New(nil)
	e := &env{t: t, srv: srv, st: st, ts: ts, c: &http.Client{Jar: jar}, data: data}
	t.Cleanup(func() { ts.Close(); st.Close() })
	return e
}

func (e *env) do(method, path string, body any, hdr map[string]string) (int, []byte) {
	e.t.Helper()
	var rd io.Reader
	if b, ok := body.([]byte); ok {
		rd = bytes.NewReader(b)
	} else if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, e.ts.URL+path, rd)
	if e.csrf != "" {
		req.Header.Set("X-CSRF-Token", e.csrf)
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := e.c.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func (e *env) login(user, pass string) int {
	code, b := e.do("POST", "/api/login", map[string]string{"username": user, "password": pass}, nil)
	if code == 200 {
		var r struct{ Csrf string }
		json.Unmarshal(b, &r)
		e.csrf = r.Csrf
	}
	return code
}

func (e *env) setup() {
	e.t.Helper()
	code, b := e.do("POST", "/api/setup", map[string]string{"username": "admin", "password": "admin-pass-1"}, nil)
	if code != 200 {
		e.t.Fatalf("setup: %d %s", code, b)
	}
	var r struct{ Csrf string }
	json.Unmarshal(b, &r)
	e.csrf = r.Csrf
}

func (e *env) addShare(name, dir string, ro bool) string {
	e.t.Helper()
	code, b := e.do("POST", "/api/shares", map[string]any{"name": name, "path": dir, "readOnly": ro}, nil)
	if code != 201 {
		e.t.Fatalf("add share: %d %s", code, b)
	}
	var s struct{ ID string }
	json.Unmarshal(b, &s)
	return s.ID
}

// second client against the same server
func (e *env) another() *env {
	jar, _ := cookiejar.New(nil)
	return &env{t: e.t, srv: e.srv, st: e.st, ts: e.ts, c: &http.Client{Jar: jar}}
}

func TestSetupLoginSession(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.do("GET", "/api/shares", nil, nil); code != 401 {
		t.Fatalf("unauthenticated shares = %d", code)
	}
	e.setup()
	if code, _ := e.do("POST", "/api/setup", map[string]string{"username": "x", "password": "another-pass"}, nil); code != 409 {
		t.Fatalf("second setup = %d", code)
	}
	if code, _ := e.do("GET", "/api/session", nil, nil); code != 200 {
		t.Fatal("session after setup")
	}
	e.do("POST", "/api/logout", map[string]string{}, nil)
	if code, _ := e.do("GET", "/api/session", nil, nil); code != 401 {
		t.Fatal("session survives logout")
	}
	if e.login("admin", "wrong-password") != 401 {
		t.Fatal("bad password accepted")
	}
	if e.login("admin", "admin-pass-1") != 200 {
		t.Fatal("good login rejected")
	}
}

func TestLoginRateLimit(t *testing.T) {
	e := newEnv(t)
	e.setup()
	e.do("POST", "/api/logout", map[string]string{}, nil)
	got429 := false
	for i := 0; i < 8; i++ {
		if e.login("admin", "bad-bad-bad") == 429 {
			got429 = true
		}
	}
	if !got429 {
		t.Fatal("no throttling after repeated failures")
	}
	if e.login("admin", "admin-pass-1") != 429 {
		t.Fatal("correct password must also be throttled during lockout")
	}
}

func TestSessionExpiry(t *testing.T) {
	e := newEnv(t)
	e.setup()
	tok, _, _ := e.st.CreateSession(1, -time.Second)
	if _, _, err := e.st.LookupSession(tok); err == nil {
		t.Fatal("expired session accepted")
	}
}

func TestCSRFAndOrigin(t *testing.T) {
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	good := e.csrf
	e.csrf = ""
	if code, _ := e.do("POST", "/api/shares", map[string]any{"name": "x", "path": dir}, nil); code != 403 {
		t.Fatalf("missing csrf = %d", code)
	}
	e.csrf = good
	if code, _ := e.do("POST", "/api/shares", map[string]any{"name": "x", "path": dir}, map[string]string{"Origin": "http://evil.example"}); code != 403 {
		t.Fatalf("foreign origin = %d", code)
	}
	if code, _ := e.do("POST", "/api/shares", map[string]any{"name": "x", "path": dir}, nil); code != 201 {
		t.Fatalf("valid request = %d", code)
	}
	if code, _ := e.do("OPTIONS", "/api/shares", nil, nil); code != 405 {
		t.Fatalf("OPTIONS = %d", code)
	}
}

func TestSharePersistenceAcrossRestart(t *testing.T) {
	data := t.TempDir()
	shared := t.TempDir()
	e := startEnv(t, data)
	e.setup()
	id := e.addShare("Docs", shared, true)
	e.ts.Close()
	e.st.Close() // simulate server stop

	e2 := startEnv(t, data)
	if e2.login("admin", "admin-pass-1") != 200 {
		t.Fatal("account lost across restart")
	}
	code, b := e2.do("GET", "/api/shares", nil, nil)
	if code != 200 || !strings.Contains(string(b), id) || !strings.Contains(string(b), `"online"`) {
		t.Fatalf("share lost across restart: %d %s", code, b)
	}
}

func TestPermissionsReadOnlyShareAndReaderRole(t *testing.T) {
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644)
	ro := e.addShare("RO", dir, true)
	rw := e.addShare("RW", t.TempDir(), false)

	if code, _ := e.do("POST", "/api/shares/"+ro+"/upload?name=x.txt", []byte("data"), nil); code != 403 {
		t.Fatalf("upload to read-only share = %d", code)
	}
	if code, _ := e.do("POST", "/api/shares/"+ro+"/delete", map[string]string{"path": "a.txt"}, nil); code != 403 {
		t.Fatalf("delete on read-only share = %d", code)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatal("file removed from read-only share")
	}
	if code, _ := e.do("POST", "/api/shares/"+rw+"/upload?name=x.txt", []byte("data"), nil); code != 201 {
		t.Fatalf("admin upload to rw share = %d", code)
	}
	if code, _ := e.do("POST", "/api/admin/users", map[string]string{"username": "kid", "password": "kid-password", "role": "reader"}, nil); code != 201 {
		t.Fatal("create reader")
	}
	k := e.another()
	if k.login("kid", "kid-password") != 200 {
		t.Fatal("reader login")
	}
	if code, _ := k.do("POST", "/api/shares/"+rw+"/upload?name=y.txt", []byte("data"), nil); code != 403 {
		t.Fatalf("reader upload = %d", code)
	}
	if code, _ := k.do("GET", "/api/shares/"+rw+"/download?path=x.txt", nil, nil); code != 200 {
		t.Fatalf("reader download = %d", code)
	}
	if code, _ := k.do("GET", "/api/admin/settings", nil, nil); code != 403 {
		t.Fatalf("reader reached admin API = %d", code)
	}
	if code, b := k.do("GET", "/api/shares", nil, nil); code != 200 || strings.Contains(string(b), `"path"`) {
		t.Fatalf("filesystem path leaked to non-admin: %s", b)
	}
}

func TestTraversalOverHTTP(t *testing.T) {
	e := newEnv(t)
	e.setup()
	outer := t.TempDir()
	os.WriteFile(filepath.Join(outer, "secret.txt"), []byte("TOP SECRET"), 0o644)
	share := filepath.Join(outer, "share")
	os.Mkdir(share, 0o755)
	id := e.addShare("S", share, false)
	for _, p := range []string{"../secret.txt", "..%2Fsecret.txt", "%2e%2e/secret.txt", "a/../../secret.txt", "..%5Csecret.txt"} {
		code, b := e.do("GET", "/api/shares/"+id+"/download?path="+p, nil, nil)
		if code == 200 || strings.Contains(string(b), "TOP SECRET") {
			t.Fatalf("traversal %q succeeded: %d", p, code)
		}
	}
	if code, _ := e.do("POST", "/api/shares/"+id+"/upload?name="+url.QueryEscape("../evil.txt"), []byte("x"), nil); code != 400 {
		t.Fatalf("upload name traversal = %d", code)
	}
	if code, _ := e.do("POST", "/api/shares/"+id+"/upload?path=..&name=evil.txt", []byte("x"), nil); code != 400 {
		t.Fatalf("upload dir traversal = %d", code)
	}
	if _, err := os.Stat(filepath.Join(outer, "evil.txt")); err == nil {
		t.Fatal("file written outside share")
	}
	if code, _ := e.do("GET", "/api/shares/not-a-valid-id/list", nil, nil); code != 404 {
		t.Fatalf("bad share id = %d", code)
	}
	if code, _ := e.do("POST", "/api/shares", map[string]any{"name": "data", "path": e.data}, nil); code != 400 {
		t.Fatalf("sharing data dir = %d", code)
	}
}

func TestUploadDownloadRoundTripUnicodeAndRange(t *testing.T) {
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	id := e.addShare("S", dir, false)
	content := make([]byte, 3<<20)
	rand.Read(content)
	name := "résumé 日本語 (1).bin"
	code, b := e.do("POST", "/api/shares/"+id+"/upload?name="+url.QueryEscape(name), content, nil)
	if code != 201 {
		t.Fatalf("upload: %d %s", code, b)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil || !bytes.Equal(got, content) {
		t.Fatal("stored file differs")
	}
	req, _ := http.NewRequest("GET", e.ts.URL+"/api/shares/"+id+"/download?path="+url.QueryEscape(name), nil)
	resp, _ := e.c.Do(req)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.Equal(body, content) {
		t.Fatal("download differs")
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.HasPrefix(cd, "attachment") || !strings.Contains(cd, "filename*=utf-8''") {
		t.Fatalf("Content-Disposition = %q", cd)
	}
	req, _ = http.NewRequest("GET", e.ts.URL+"/api/shares/"+id+"/download?path="+url.QueryEscape(name), nil)
	req.Header.Set("Range", "bytes=100-199")
	resp, _ = e.c.Do(req)
	part, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 206 || !bytes.Equal(part, content[100:200]) {
		t.Fatalf("range: %d len=%d", resp.StatusCode, len(part))
	}
}

func TestUploadConflictAndLimit(t *testing.T) {
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	id := e.addShare("S", dir, false)
	up := func(q string, body string) (int, string) {
		code, b := e.do("POST", "/api/shares/"+id+"/upload?name=f.txt"+q, []byte(body), nil)
		return code, string(b)
	}
	if c, _ := up("", "one"); c != 201 {
		t.Fatal("first upload")
	}
	if c, _ := up("", "two"); c != 409 {
		t.Fatalf("duplicate without policy = %d", c)
	}
	if c, b := up("&conflict=rename", "two"); c != 201 || !strings.Contains(b, "f (1).txt") {
		t.Fatalf("rename policy = %d %s", c, b)
	}
	if c, _ := up("&conflict=replace", "three"); c != 201 {
		t.Fatal("replace policy")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(b) != "three" {
		t.Fatalf("content after replace = %q", b)
	}
	s := e.srv.settings()
	s.MaxUploadBytes = 10
	e.do("PUT", "/api/admin/settings", s, nil)
	if c, _ := up("&conflict=replace", strings.Repeat("x", 100)); c != 413 {
		t.Fatalf("over limit = %d", c)
	}
	if left, _ := os.ReadDir(filepath.Join(dir, ".homeserver-tmp")); len(left) != 0 {
		t.Fatalf("temp files left after rejected upload: %d", len(left))
	}
}

type dropBody struct {
	r io.Reader
	n int
}

func (s *dropBody) Read(p []byte) (int, error) {
	if s.n > 0 {
		return 0, io.ErrUnexpectedEOF
	}
	n, err := s.r.Read(p)
	s.n += n
	return n, err
}

func TestInterruptedUploadLeavesNothing(t *testing.T) {
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	id := e.addShare("S", dir, false)
	// Declare 1 MiB but fail after 100 KiB: a dropped connection.
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/shares/"+id+"/upload?name=partial.bin",
		io.NopCloser(&dropBody{r: bytes.NewReader(make([]byte, 100<<10))}))
	req.ContentLength = 1 << 20
	req.Header.Set("X-CSRF-Token", e.csrf)
	if resp, err := e.c.Do(req); err == nil {
		resp.Body.Close()
	}
	time.Sleep(300 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(dir, "partial.bin")); err == nil {
		t.Fatal("partial file was finalised")
	}
	if left, _ := os.ReadDir(filepath.Join(dir, ".homeserver-tmp")); len(left) != 0 {
		t.Fatalf("temp files left after interrupted upload: %d", len(left))
	}
}

func TestFileCRUD(t *testing.T) {
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	id := e.addShare("S", dir, false)
	post := func(op string, body map[string]string) int {
		c, _ := e.do("POST", "/api/shares/"+id+"/"+op, body, nil)
		return c
	}
	if post("mkdir", map[string]string{"path": "", "name": "sub"}) != 200 {
		t.Fatal("mkdir")
	}
	if post("mkdir", map[string]string{"path": "", "name": "sub"}) != 409 {
		t.Fatal("duplicate mkdir should conflict")
	}
	e.do("POST", "/api/shares/"+id+"/upload?path=sub&name=a.txt", []byte("A"), nil)
	if post("rename", map[string]string{"path": "sub/a.txt", "name": "b.txt"}) != 200 {
		t.Fatal("rename")
	}
	if post("mkdir", map[string]string{"path": "", "name": "dest"}) != 200 || post("move", map[string]string{"path": "sub/b.txt", "dest": "dest"}) != 200 {
		t.Fatal("move")
	}
	code, b := e.do("GET", "/api/shares/"+id+"/list?path=dest", nil, nil)
	if code != 200 || !strings.Contains(string(b), "b.txt") {
		t.Fatalf("list: %d %s", code, b)
	}
	if post("delete", map[string]string{"path": "dest"}) != 200 {
		t.Fatal("delete")
	}
	if _, err := os.Stat(filepath.Join(dir, "dest")); !os.IsNotExist(err) {
		t.Fatal("not deleted")
	}
	e.do("DELETE", "/api/shares/"+id, nil, nil)
	if _, err := os.Stat(filepath.Join(dir, "sub")); err != nil {
		t.Fatal("removing a share deleted its files")
	}
	if code, _ := e.do("GET", "/api/shares/"+id+"/list", nil, nil); code != 404 {
		t.Fatalf("removed share still reachable: %d", code)
	}
}

func TestUnavailableShareRecovers(t *testing.T) {
	e := newEnv(t)
	e.setup()
	parent := t.TempDir()
	dir := filepath.Join(parent, "usb")
	os.Mkdir(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x"), 0o644)
	id := e.addShare("USB", dir, true)
	os.Rename(dir, dir+".gone") // drive unplugged
	if code, _ := e.do("GET", "/api/shares/"+id+"/list", nil, nil); code != 503 {
		t.Fatalf("missing folder = %d", code)
	}
	if _, b := e.do("GET", "/api/shares", nil, nil); !strings.Contains(string(b), `"offline"`) {
		t.Fatalf("status not offline: %s", b)
	}
	os.Rename(dir+".gone", dir) // plugged back in
	if code, _ := e.do("GET", "/api/shares/"+id+"/list", nil, nil); code != 200 {
		t.Fatalf("share did not recover: %d", code)
	}
}

func TestConcurrentTransfers(t *testing.T) {
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	id := e.addShare("S", dir, false)
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data := bytes.Repeat([]byte{byte(i)}, 512<<10)
			req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/shares/%s/upload?name=f%d.bin", e.ts.URL, id, i), bytes.NewReader(data))
			req.Header.Set("X-CSRF-Token", e.csrf)
			resp, err := e.c.Do(req)
			if err != nil {
				errs <- err
				return
			}
			resp.Body.Close()
			if resp.StatusCode != 201 {
				errs <- fmt.Errorf("upload %d: %d", i, resp.StatusCode)
				return
			}
			got, _ := os.ReadFile(filepath.Join(dir, fmt.Sprintf("f%d.bin", i)))
			if !bytes.Equal(got, data) {
				errs <- fmt.Errorf("content mismatch %d", i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestSettingsAndAdminGuards(t *testing.T) {
	e := newEnv(t)
	e.setup()
	s := e.srv.settings()
	s.Port = 70000
	if code, _ := e.do("PUT", "/api/admin/settings", s, nil); code != 400 {
		t.Fatalf("invalid port accepted: %d", code)
	}
	s.Port = 9999
	code, b := e.do("PUT", "/api/admin/settings", s, nil)
	if code != 200 || !strings.Contains(string(b), `"restartRequired":true`) {
		t.Fatalf("settings: %d %s", code, b)
	}
	if code, _ := e.do("PATCH", "/api/admin/users/1", map[string]any{"role": "reader"}, nil); code != 409 {
		t.Fatalf("demoting last admin = %d", code)
	}
	if code, _ := e.do("DELETE", "/api/admin/users/1", nil, nil); code != 409 {
		t.Fatalf("deleting last admin = %d", code)
	}
}

func TestSecurityHeaders(t *testing.T) {
	e := newEnv(t)
	resp, err := http.Get(e.ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Content-Security-Policy") == "" {
		t.Fatal("security headers missing")
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS header must not be set")
	}
}

func TestBoundedMemoryLargeTransfer(t *testing.T) {
	if testing.Short() {
		t.Skip("large transfer")
	}
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	id := e.addShare("S", dir, false)
	const size = 256 << 20
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	pr, pw := io.Pipe()
	go func() {
		chunk := make([]byte, 1<<20)
		for i := 0; i < size>>20; i++ {
			if _, err := pw.Write(chunk); err != nil {
				return
			}
		}
		pw.Close()
	}()
	req, _ := http.NewRequest("POST", e.ts.URL+"/api/shares/"+id+"/upload?name=big.bin", pr)
	req.ContentLength = size
	req.Header.Set("X-CSRF-Token", e.csrf)
	resp, err := e.c.Do(req)
	if err != nil || resp.StatusCode != 201 {
		t.Fatalf("big upload: %v %v", err, resp)
	}
	resp.Body.Close()
	resp, _ = e.c.Get(fmt.Sprintf("%s/api/shares/%s/download?path=big.bin", e.ts.URL, id))
	n, _ := io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if n != size {
		t.Fatalf("downloaded %d", n)
	}
	runtime.ReadMemStats(&after)
	t.Logf("256 MiB up+down: HeapAlloc before=%d KiB after=%d KiB, TotalAlloc delta=%d MiB",
		before.HeapAlloc>>10, after.HeapAlloc>>10, (after.TotalAlloc-before.TotalAlloc)>>20)
	if after.HeapAlloc > before.HeapAlloc+64<<20 {
		t.Fatalf("heap grew by %d MiB during a 256 MiB transfer", (after.HeapAlloc-before.HeapAlloc)>>20)
	}
}

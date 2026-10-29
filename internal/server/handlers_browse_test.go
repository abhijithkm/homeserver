package server

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func browse(t *testing.T, e *env, path string) (int, browseResult) {
	t.Helper()
	code, b := e.do("GET", "/api/admin/browse?path="+url.QueryEscape(path), nil, nil)
	var res browseResult
	if code == 200 {
		if err := json.Unmarshal(b, &res); err != nil {
			t.Fatalf("bad json: %v: %s", err, b)
		}
	}
	return code, res
}

func TestBrowseListsOnlyFoldersSorted(t *testing.T) {
	e := newEnv(t)
	e.setup()
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "zeta"), 0o755)
	os.Mkdir(filepath.Join(dir, "Alpha"), 0o755)
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("x"), 0o644)

	code, res := browse(t, e, dir)
	if code != 200 {
		t.Fatalf("browse = %d", code)
	}
	if len(res.Entries) != 2 || res.Entries[0].Name != "Alpha" || res.Entries[1].Name != "zeta" {
		t.Fatalf("want [Alpha zeta], got %+v", res.Entries)
	}
	if res.Entries[0].Path != filepath.Join(dir, "Alpha") || !res.Entries[0].Readable {
		t.Fatalf("bad entry %+v", res.Entries[0])
	}
	if res.Path != filepath.Clean(dir) || res.Parent != filepath.Dir(dir) {
		t.Fatalf("bad path/parent: %q %q", res.Path, res.Parent)
	}
}

func TestBrowseRootListsDrivesAndRootParentIsEmpty(t *testing.T) {
	e := newEnv(t)
	e.setup()
	code, res := browse(t, e, "")
	if code != 200 || res.Path != "" || len(res.Entries) == 0 {
		t.Fatalf("roots: code=%d res=%+v", code, res)
	}
	root := res.Entries[0].Path
	if code, res = browse(t, e, root); code == 200 && res.Parent != "" {
		t.Fatalf("parent of %q should be empty (back to drives), got %q", root, res.Parent)
	}
}

func TestBrowseRejectsBadPaths(t *testing.T) {
	e := newEnv(t)
	e.setup()
	if code, _ := browse(t, e, `relative\dir`); code != 400 {
		t.Fatalf("relative path = %d", code)
	}
	if code, _ := browse(t, e, filepath.Join(t.TempDir(), "missing")); code != 404 {
		t.Fatalf("missing folder = %d", code)
	}
	f := filepath.Join(t.TempDir(), "f.txt")
	os.WriteFile(f, []byte("x"), 0o644)
	if code, _ := browse(t, e, f); code == 200 {
		t.Fatal("listing a file succeeded")
	}
}

func TestBrowseIsAdminOnly(t *testing.T) {
	e := newEnv(t)
	e.setup()
	if code, _ := e.do("POST", "/api/admin/users", map[string]string{"username": "kid", "password": "kid-password", "role": "writer"}, nil); code != 201 {
		t.Fatal("create writer")
	}
	k := e.another()
	if k.login("kid", "kid-password") != 200 {
		t.Fatal("writer login")
	}
	if code, _ := browse(t, k, t.TempDir()); code != 403 {
		t.Fatalf("writer browse = %d", code)
	}
	anon := e.another()
	if code, _ := browse(t, anon, t.TempDir()); code != 401 {
		t.Fatalf("anonymous browse = %d", code)
	}
}

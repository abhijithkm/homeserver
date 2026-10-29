package fsx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func newRoot(t *testing.T) (*Root, string) {
	t.Helper()
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r, dir
}

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestUnreadableExplainsPermissionErrors(t *testing.T) {
	raw := &os.PathError{Op: "CreateFile", Path: `C:\Users\me\Downloads`, Err: os.ErrPermission}
	msg := unreadable(`C:\Users\me\Downloads`, raw).Error()
	for _, want := range []string{"service account cannot read", `homeserver.exe grant "C:\Users\me\Downloads"`, "--readonly"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q lacks %q", msg, want)
		}
	}
	if strings.Contains(msg, "CreateFile") {
		t.Errorf("raw system error leaked into the message: %q", msg)
	}
	other := unreadable(`D:\x`, errors.New("disk on fire")).Error()
	if !strings.Contains(other, "disk on fire") {
		t.Errorf("non-permission errors should keep their cause: %q", other)
	}
}

func TestValidateNameRejects(t *testing.T) {
	bad := []string{"", ".", "..", "a/b", `a\b`, "a:b", "file.", "file ", "CON", "nul.txt", "com1", "LPT9.log", "a*b", "a\x00b", TempDirName, strings.Repeat("x", 256)}
	for _, n := range bad {
		if ValidateName(n) == nil {
			t.Errorf("ValidateName(%q) accepted", n)
		}
	}
	for _, n := range []string{"ok.txt", "résumé.pdf", "日本語", "a b", ".hidden", "com10"} {
		if err := ValidateName(n); err != nil {
			t.Errorf("ValidateName(%q) = %v", n, err)
		}
	}
}

func TestTraversalRejected(t *testing.T) {
	r, dir := newRoot(t)
	write(t, filepath.Join(filepath.Dir(dir), "secret.txt"), "secret")
	for _, p := range []string{"../secret.txt", "a/../../secret.txt", "..", "/../x", "a/./b", `..\secret.txt`, "C:/Windows"} {
		if _, _, err := r.OpenFile(p); !errors.Is(err, ErrUnsafe) {
			t.Errorf("OpenFile(%q) err=%v, want ErrUnsafe", p, err)
		}
		if _, err := r.List(p, ListOptions{}); !errors.Is(err, ErrUnsafe) {
			t.Errorf("List(%q) err=%v, want ErrUnsafe", p, err)
		}
	}
	if err := r.Remove(""); !errors.Is(err, ErrUnsafe) {
		t.Errorf("removing root: %v", err)
	}
}

func TestLinkContainment(t *testing.T) {
	r, dir := newRoot(t)
	outside := t.TempDir()
	write(t, filepath.Join(outside, "secret.txt"), "secret")
	link := filepath.Join(dir, "evil")
	var err error
	if runtime.GOOS == "windows" {
		err = exec.Command("cmd", "/c", "mklink", "/J", link, outside).Run() // junction: no admin needed
	} else {
		err = os.Symlink(outside, link)
	}
	if err != nil {
		t.Skipf("cannot create link: %v", err)
	}
	if _, _, err := r.OpenFile("evil/secret.txt"); err == nil {
		t.Fatal("read through link succeeded")
	}
	if _, err := r.List("evil", ListOptions{}); err == nil {
		t.Fatal("list through link succeeded")
	}
	if err := r.Remove("evil/secret.txt"); err == nil {
		t.Fatal("remove through link succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "secret.txt")); err != nil {
		t.Fatal("outside file was touched")
	}
	res, err := r.List("", ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range res.Entries {
		if e.Name == "evil" {
			t.Fatal("link listed")
		}
	}
}

func TestListSortSearchPaging(t *testing.T) {
	r, dir := newRoot(t)
	for i := 0; i < 25; i++ {
		write(t, filepath.Join(dir, fmt.Sprintf("f%02d.txt", i)), strings.Repeat("x", i))
	}
	write(t, filepath.Join(dir, "match-me.log"), "")
	if err := os.Mkdir(filepath.Join(dir, "zdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := r.List("", ListOptions{Sort: "name", Limit: 10})
	if err != nil || res.Total != 27 || len(res.Entries) != 10 {
		t.Fatalf("got total=%d n=%d err=%v", res.Total, len(res.Entries), err)
	}
	if !res.Entries[0].IsDir || res.Entries[0].Name != "zdir" {
		t.Fatalf("folders must come first: %+v", res.Entries[0])
	}
	p2, _ := r.List("", ListOptions{Sort: "name", Limit: 10, Offset: 10})
	if p2.Entries[0].Name != "f09.txt" {
		t.Fatalf("page 2 starts at %s", p2.Entries[0].Name)
	}
	bySize, _ := r.List("", ListOptions{Sort: "size", Desc: true, Limit: 3})
	if bySize.Entries[1].Name != "f24.txt" {
		t.Fatalf("size desc: %+v", bySize.Entries)
	}
	q, _ := r.List("", ListOptions{Query: "MATCH"})
	if q.Total != 1 || q.Entries[0].Name != "match-me.log" {
		t.Fatalf("search: %+v", q)
	}
	if _, err := r.List("", ListOptions{Offset: MaxWindow, Limit: 10}); !errors.Is(err, ErrTooBig) {
		t.Fatalf("expected ErrTooBig, got %v", err)
	}
}

func TestUploadConflictPolicies(t *testing.T) {
	r, dir := newRoot(t)
	write(t, filepath.Join(dir, "a.txt"), "old")
	up := func(policy, content string) (string, error) {
		u, err := r.BeginUpload("", "a.txt")
		if err != nil {
			return "", err
		}
		u.File.WriteString(content)
		return u.Commit(policy)
	}
	if _, err := up(ConflictFail, "new"); !errors.Is(err, ErrExists) {
		t.Fatalf("fail policy: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "old" {
		t.Fatal("existing file overwritten by fail policy")
	}
	n, err := up(ConflictRename, "renamed")
	if err != nil || n != "a (1).txt" {
		t.Fatalf("rename policy: %q %v", n, err)
	}
	if _, err := up(ConflictReplace, "replaced"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "replaced" {
		t.Fatalf("replace policy content %q", b)
	}
	left, _ := os.ReadDir(filepath.Join(dir, TempDirName))
	if len(left) != 0 {
		t.Fatalf("temp files left: %d", len(left))
	}
}

func TestUploadAbortAndCleanTemp(t *testing.T) {
	r, dir := newRoot(t)
	u, err := r.BeginUpload("", "x.bin")
	if err != nil {
		t.Fatal(err)
	}
	u.Abort()
	if left, _ := os.ReadDir(filepath.Join(dir, TempDirName)); len(left) != 0 {
		t.Fatal("abort left temp file")
	}
	u2, _ := r.BeginUpload("", "y.bin")
	u2.File.Close() // simulate crash: handle gone, file stays
	old := time.Now().Add(-48 * time.Hour)
	left, _ := os.ReadDir(filepath.Join(dir, TempDirName))
	os.Chtimes(filepath.Join(dir, TempDirName, left[0].Name()), old, old)
	if n := r.CleanTemp(24 * time.Hour); n != 1 {
		t.Fatalf("CleanTemp removed %d", n)
	}
	res, _ := r.List("", ListOptions{})
	for _, e := range res.Entries {
		if e.Name == TempDirName {
			t.Fatal("temp dir visible in listing")
		}
	}
}

func TestMkdirRenameMoveRemove(t *testing.T) {
	r, dir := newRoot(t)
	write(t, filepath.Join(dir, "a", "f.txt"), "1")
	if err := r.Mkdir("", "b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Mkdir("", "b"); !errors.Is(err, ErrExists) {
		t.Fatalf("dup mkdir: %v", err)
	}
	if err := r.Move("a/f.txt", "b"); err != nil {
		t.Fatal(err)
	}
	if err := r.Move("b", "b"); err == nil {
		t.Fatal("moved folder into itself")
	}
	write(t, filepath.Join(dir, "b", "g.txt"), "2")
	if err := r.Rename("b/g.txt", "f.txt"); !errors.Is(err, ErrExists) {
		t.Fatalf("rename onto existing: %v", err)
	}
	if err := r.Rename("b/g.txt", "../evil"); err == nil {
		t.Fatal("rename with traversal accepted")
	}
	if err := r.Remove("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "b")); !os.IsNotExist(err) {
		t.Fatal("dir not removed")
	}
}

func TestUnavailableRoot(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "gone"))
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("got %v", err)
	}
}

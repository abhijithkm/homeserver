// Package fsx implements contained filesystem access for a share.
//
// Every operation opens the share root afresh (revalidating that it exists),
// resolves client-supplied relative paths through os.Root so that nothing can
// escape the root, and additionally refuses any path whose components are
// symlinks, junctions or other reparse points.
package fsx

import (
	"container/heap"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// TempDirName is the per-share hidden directory used for in-flight uploads.
const TempDirName = ".homeserver-tmp"

// MaxWindow caps offset+limit for directory listings so memory stays bounded.
const MaxWindow = 10000

// Sentinel errors mapped to HTTP statuses by the server layer.
var (
	ErrUnsafe      = errors.New("unsafe path")
	ErrNotFound    = errors.New("not found")
	ErrExists      = errors.New("already exists")
	ErrUnavailable = errors.New("share folder unavailable")
	ErrNotDir      = errors.New("not a directory")
	ErrIsDir       = errors.New("is a directory")
	ErrDenied      = errors.New("permission denied by filesystem")
	ErrNoSpace     = errors.New("no space left on device")
	ErrTooBig      = errors.New("directory window too large; use search or sort to narrow")
)

var reserved = map[string]bool{"con": true, "prn": true, "aux": true, "nul": true}

// ValidateName checks a single path segment.
func ValidateName(n string) error {
	if n == "" || n == "." || n == ".." || len(n) > 255 {
		return ErrUnsafe
	}
	if strings.EqualFold(n, TempDirName) {
		return ErrUnsafe
	}
	if strings.HasSuffix(n, ".") || strings.HasSuffix(n, " ") {
		return ErrUnsafe
	}
	for _, r := range n {
		if r < 0x20 || r == 0x7f || strings.ContainsRune(`\/:*?"<>|`, r) {
			return ErrUnsafe
		}
	}
	base := strings.ToLower(n)
	if i := strings.IndexByte(base, '.'); i >= 0 {
		base = base[:i]
	}
	if reserved[base] {
		return ErrUnsafe
	}
	if len(base) == 4 && (strings.HasPrefix(base, "com") || strings.HasPrefix(base, "lpt")) && base[3] >= '0' && base[3] <= '9' {
		return ErrUnsafe
	}
	return nil
}

// Clean validates a slash-separated relative path and returns its segments.
// "" and "/" denote the share root.
func Clean(rel string) ([]string, error) {
	rel = strings.Trim(rel, "/")
	if rel == "" {
		return nil, nil
	}
	segs := strings.Split(rel, "/")
	for _, s := range segs {
		if err := ValidateName(s); err != nil {
			return nil, err
		}
	}
	return segs, nil
}

// Root is an open handle on a share root.
type Root struct{ r *os.Root }

// Open opens the share root. It fails with ErrUnavailable when the folder is
// missing (e.g. removable drive unplugged) or not a directory.
func Open(path string) (*Root, error) {
	r, err := os.OpenRoot(path)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return nil, fmt.Errorf("%w: %v", ErrDenied, err)
		}
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if fi, err := r.Stat("."); err != nil || !fi.IsDir() {
		r.Close()
		return nil, ErrUnavailable
	}
	return &Root{r}, nil
}

// Close releases the handle.
func (r *Root) Close() error { return r.r.Close() }

func osPath(segs []string) string {
	if len(segs) == 0 {
		return "."
	}
	return filepath.Join(segs...)
}

func isLink(m fs.FileMode) bool { return m&(fs.ModeSymlink|fs.ModeIrregular) != 0 }

// resolve confirms no component of segs is a link/reparse point.
func (r *Root) resolve(segs []string) error {
	for i := range segs {
		fi, err := r.r.Lstat(osPath(segs[:i+1]))
		if err != nil {
			return mapErr(err)
		}
		if isLink(fi.Mode()) {
			return ErrUnsafe
		}
		if i < len(segs)-1 && !fi.IsDir() {
			return ErrNotDir
		}
	}
	return nil
}

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	var en syscall.Errno
	if errors.As(err, &en) {
		switch uintptr(en) {
		case 112, 39, uintptr(syscall.ENOSPC): // ERROR_DISK_FULL, ERROR_HANDLE_DISK_FULL
			return ErrNoSpace
		}
	}
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return ErrNotFound
	case errors.Is(err, fs.ErrExist):
		return ErrExists
	case errors.Is(err, fs.ErrPermission):
		return ErrDenied
	case strings.Contains(err.Error(), "path escapes"):
		return ErrUnsafe
	}
	return err
}

// Entry is one directory item.
type Entry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`
	ModTime int64  `json:"modTime"` // unix milliseconds
	Type    string `json:"type"`    // lowercase extension, or "folder"
}

// ListOptions control a directory listing window.
type ListOptions struct {
	Sort   string // name | size | type | modified
	Desc   bool
	Query  string // case-insensitive substring filter on name
	Offset int
	Limit  int
}

// ListResult is one page of a directory.
type ListResult struct {
	Entries []Entry `json:"entries"`
	Total   int     `json:"total"` // entries matching the query
}

type entryHeap struct {
	items []Entry
	less  func(a, b Entry) bool
}

func (h entryHeap) Len() int           { return len(h.items) }
func (h entryHeap) Less(i, j int) bool { return h.less(h.items[j], h.items[i]) } // max-heap
func (h entryHeap) Swap(i, j int)      { h.items[i], h.items[j] = h.items[j], h.items[i] }
func (h *entryHeap) Push(x any)        { h.items = append(h.items, x.(Entry)) }
func (h *entryHeap) Pop() any {
	n := len(h.items)
	x := h.items[n-1]
	h.items = h.items[:n-1]
	return x
}

func comparator(sortKey string, desc bool) func(a, b Entry) bool {
	return func(a, b Entry) bool {
		if a.IsDir != b.IsDir {
			return a.IsDir // folders first, regardless of direction
		}
		var c int
		switch sortKey {
		case "size":
			c = cmp64(a.Size, b.Size)
		case "modified":
			c = cmp64(a.ModTime, b.ModTime)
		case "type":
			c = strings.Compare(a.Type, b.Type)
		}
		if c != 0 {
			if desc {
				return c > 0
			}
			return c < 0
		}
		c = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		if c == 0 {
			c = strings.Compare(a.Name, b.Name)
		}
		if sortKey == "name" && desc {
			return c > 0
		}
		return c < 0
	}
}

func cmp64(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// List returns a window of a directory using bounded memory: entries are
// streamed in batches and only the top offset+limit are retained.
func (r *Root) List(rel string, o ListOptions) (ListResult, error) {
	segs, err := Clean(rel)
	if err != nil {
		return ListResult{}, err
	}
	if o.Limit <= 0 || o.Limit > 1000 {
		o.Limit = 200
	}
	if o.Offset < 0 {
		o.Offset = 0
	}
	if o.Offset+o.Limit > MaxWindow {
		return ListResult{}, ErrTooBig
	}
	if err := r.resolve(segs); err != nil {
		return ListResult{}, err
	}
	d, err := r.r.Open(osPath(segs))
	if err != nil {
		return ListResult{}, mapErr(err)
	}
	defer d.Close()
	if fi, err := d.Stat(); err != nil || !fi.IsDir() {
		return ListResult{}, ErrNotDir
	}
	keep := o.Offset + o.Limit
	h := &entryHeap{less: comparator(o.Sort, o.Desc)}
	q := strings.ToLower(o.Query)
	total := 0
	for {
		batch, err := d.ReadDir(512)
		for _, de := range batch {
			name := de.Name()
			if len(segs) == 0 && strings.EqualFold(name, TempDirName) {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(name), q) {
				continue
			}
			if de.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
				continue // links and reparse points are never exposed
			}
			fi, ierr := de.Info()
			if ierr != nil {
				continue // vanished between readdir and stat
			}
			e := Entry{Name: name, IsDir: fi.IsDir(), ModTime: fi.ModTime().UnixMilli()}
			if e.IsDir {
				e.Type = "folder"
			} else {
				e.Size = fi.Size()
				e.Type = strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))
			}
			total++
			if len(h.items) < keep {
				heap.Push(h, e)
			} else if h.less(e, h.items[0]) {
				h.items[0] = e
				heap.Fix(h, 0)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ListResult{}, mapErr(err)
		}
	}
	sort.Slice(h.items, func(i, j int) bool { return h.less(h.items[i], h.items[j]) })
	res := ListResult{Total: total, Entries: []Entry{}}
	if o.Offset < len(h.items) {
		res.Entries = h.items[o.Offset:]
	}
	return res, nil
}

// Stat returns info about a path (not following links).
func (r *Root) Stat(rel string) (fs.FileInfo, error) {
	segs, err := Clean(rel)
	if err != nil {
		return nil, err
	}
	if err := r.resolve(segs); err != nil {
		return nil, err
	}
	fi, err := r.r.Lstat(osPath(segs))
	return fi, mapErr(err)
}

// OpenFile opens a regular file for reading.
func (r *Root) OpenFile(rel string) (*os.File, fs.FileInfo, error) {
	segs, err := Clean(rel)
	if err != nil || len(segs) == 0 {
		return nil, nil, ErrUnsafe
	}
	if err := r.resolve(segs); err != nil {
		return nil, nil, err
	}
	f, err := r.r.Open(osPath(segs))
	if err != nil {
		return nil, nil, mapErr(err)
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, mapErr(err)
	}
	if fi.IsDir() {
		f.Close()
		return nil, nil, ErrIsDir
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, nil, ErrUnsafe
	}
	return f, fi, nil
}

// child validates parent (must be an existing real directory) and name.
func (r *Root) child(parent, name string) ([]string, error) {
	if err := ValidateName(name); err != nil {
		return nil, err
	}
	ps, err := Clean(parent)
	if err != nil {
		return nil, err
	}
	if err := r.resolve(ps); err != nil {
		return nil, err
	}
	if fi, err := r.r.Lstat(osPath(ps)); err != nil || !fi.IsDir() {
		return nil, ErrNotDir
	}
	return append(ps, name), nil
}

// Mkdir creates a directory inside an existing parent directory.
func (r *Root) Mkdir(parent, name string) error {
	segs, err := r.child(parent, name)
	if err != nil {
		return err
	}
	return mapErr(r.r.Mkdir(osPath(segs), 0o755))
}

// Rename renames an item within its parent directory. Never overwrites.
func (r *Root) Rename(rel, newName string) error {
	segs, err := Clean(rel)
	if err != nil || len(segs) == 0 {
		return ErrUnsafe
	}
	dst := append(append([]string{}, segs[:len(segs)-1]...), newName)
	return r.moveSegs(segs, dst)
}

// Move moves an item into another directory of the same share. Never overwrites.
func (r *Root) Move(rel, destDir string) error {
	segs, err := Clean(rel)
	if err != nil || len(segs) == 0 {
		return ErrUnsafe
	}
	dst, err := r.child(destDir, segs[len(segs)-1])
	if err != nil {
		return err
	}
	return r.moveSegs(segs, dst)
}

func (r *Root) moveSegs(src, dst []string) error {
	if err := ValidateName(dst[len(dst)-1]); err != nil {
		return err
	}
	if err := r.resolve(src); err != nil {
		return err
	}
	if err := r.resolve(dst[:len(dst)-1]); err != nil {
		return err
	}
	s, d := strings.ToLower(strings.Join(src, "/")), strings.ToLower(strings.Join(dst, "/"))
	if strings.HasPrefix(d, s+"/") {
		return fmt.Errorf("%w: cannot move a folder into itself", ErrUnsafe)
	}
	if _, err := r.r.Lstat(osPath(dst)); err == nil && s != d {
		return ErrExists // a case-only rename of the same item is allowed
	}
	return mapErr(r.r.Rename(osPath(src), osPath(dst)))
}

// Remove deletes a file or (recursively) a directory. The share root cannot be removed.
func (r *Root) Remove(rel string) error {
	segs, err := Clean(rel)
	if err != nil || len(segs) == 0 {
		return ErrUnsafe
	}
	if err := r.resolve(segs); err != nil {
		return err
	}
	return mapErr(r.r.RemoveAll(osPath(segs)))
}

// Conflict policies for uploads.
const (
	ConflictFail    = "fail"
	ConflictRename  = "rename"
	ConflictReplace = "replace"
)

// Upload is an in-flight upload writing to a temp file.
type Upload struct {
	r        *Root
	File     *os.File
	tmp      string
	destDir  []string
	destName string
	done     bool
}

// BeginUpload creates a temp file for a new upload of name into dir.
func (r *Root) BeginUpload(dir, name string) (*Upload, error) {
	dst, err := r.child(dir, name)
	if err != nil {
		return nil, err
	}
	if err := r.r.Mkdir(TempDirName, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return nil, mapErr(err)
	}
	if fi, err := r.r.Lstat(TempDirName); err != nil || !fi.IsDir() || isLink(fi.Mode()) {
		return nil, ErrUnsafe
	}
	var f *os.File
	var tmp string
	for try := 0; ; try++ {
		var rnd [8]byte
		if _, err := rand.Read(rnd[:]); err != nil {
			return nil, err
		}
		tmp = filepath.Join(TempDirName, "u-"+hex.EncodeToString(rnd[:])+".part")
		f, err = r.r.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			break
		}
		if !errors.Is(err, fs.ErrExist) || try > 5 {
			return nil, mapErr(err)
		}
	}
	return &Upload{r: r, File: f, tmp: tmp, destDir: dst[:len(dst)-1], destName: name}, nil
}

// Abort discards the temp file.
func (u *Upload) Abort() {
	if u.done {
		return
	}
	u.done = true
	u.File.Close()
	_ = u.r.r.Remove(u.tmp)
}

// Commit finalises the upload under the given conflict policy and returns the
// final file name.
func (u *Upload) Commit(policy string) (string, error) {
	if u.done {
		return "", errors.New("upload finished")
	}
	u.done = true
	fail := func(err error) (string, error) {
		_ = u.r.r.Remove(u.tmp)
		return "", err
	}
	if err := u.File.Close(); err != nil {
		return fail(mapErr(err))
	}
	if err := u.r.resolve(u.destDir); err != nil {
		return fail(err)
	}
	dir := func(n string) string { return osPath(append(append([]string{}, u.destDir...), n)) }
	name := u.destName
	switch policy {
	case ConflictReplace:
		if fi, err := u.r.r.Lstat(dir(name)); err == nil && (fi.IsDir() || isLink(fi.Mode())) {
			return fail(ErrIsDir)
		}
	case ConflictFail, ConflictRename:
		// Atomically reserve a free name with O_EXCL, then rename over it.
		for i := 0; ; i++ {
			cand := name
			if i > 0 {
				if policy == ConflictFail || i > 999 {
					return fail(ErrExists)
				}
				cand = numbered(name, i)
			}
			ph, err := u.r.r.OpenFile(dir(cand), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err == nil {
				ph.Close()
				name = cand
				break
			}
			if !errors.Is(err, fs.ErrExist) {
				return fail(mapErr(err))
			}
			if policy == ConflictFail {
				return fail(ErrExists)
			}
		}
	default:
		return fail(errors.New("invalid conflict policy"))
	}
	if err := u.r.r.Rename(u.tmp, dir(name)); err != nil {
		if policy != ConflictReplace {
			_ = u.r.r.Remove(dir(name)) // drop placeholder
		}
		return fail(mapErr(err))
	}
	return name, nil
}

func numbered(name string, i int) string {
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	if base == "" {
		return fmt.Sprintf("%s (%d)", name, i)
	}
	return fmt.Sprintf("%s (%d)%s", base, i, ext)
}

// CleanTemp removes abandoned partial uploads older than maxAge. It touches
// only the share-root temp directory, never the rest of the tree.
func (r *Root) CleanTemp(maxAge time.Duration) (removed int) {
	d, err := r.r.Open(TempDirName)
	if err != nil {
		return 0
	}
	ents, _ := d.ReadDir(-1)
	d.Close()
	for _, e := range ents {
		fi, err := e.Info()
		if err != nil || !fi.Mode().IsRegular() {
			continue
		}
		if time.Since(fi.ModTime()) >= maxAge && r.r.Remove(filepath.Join(TempDirName, e.Name())) == nil {
			removed++
		}
	}
	return removed
}

// ValidateRootPath checks a path a user wants to register as a share and
// returns its absolute form.
func ValidateRootPath(p string) (string, error) {
	if p == "" {
		return "", errors.New("path required")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	fi, err := os.Stat(abs)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", errors.New("folder does not exist")
		}
		return "", unreadable(abs, err)
	}
	if !fi.IsDir() {
		return "", errors.New("path is not a folder")
	}
	d, err := os.Open(abs)
	if err != nil {
		return "", unreadable(abs, err)
	}
	_, rerr := d.ReadDir(1)
	d.Close()
	if rerr != nil && rerr != io.EOF {
		return "", unreadable(abs, rerr)
	}
	return abs, nil
}

// unreadable explains why a folder cannot be shared. A permission error is the
// common case (the service runs as LocalService, which cannot read folders such
// as C:\Users\<name>\Downloads), so it says how to fix it instead of showing the
// raw system error.
func unreadable(abs string, err error) error {
	if errors.Is(err, fs.ErrPermission) {
		return fmt.Errorf(`the HomeServer service account cannot read this folder. In an administrator terminal run: homeserver.exe grant "%s" (add --readonly for a read-only share), then add the share again`, abs)
	}
	return fmt.Errorf("folder is not readable by the HomeServer account: %w", err)
}

// MapErr converts an OS error into one of the package's sentinel errors
// (for example a full disk into ErrNoSpace) when it recognises it.
func MapErr(err error) error { return mapErr(err) }

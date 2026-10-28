package server

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/abhijithkm/homeserver/internal/fsx"
	"github.com/abhijithkm/homeserver/internal/store"
)

// openShare authorises access to a share and opens its root. The folder is
// re-validated on every call, so a vanished or replaced folder is noticed
// immediately and recovers on its own when it returns.
func (s *Server) openShare(w http.ResponseWriter, r *http.Request, p *principal, write bool) (store.Share, *fsx.Root, bool) {
	sh, err := s.st.GetShare(r.PathValue("id"))
	if err != nil || (!sh.Enabled && !p.User.IsAdmin()) {
		httpErr(w, http.StatusNotFound, "share not found")
		return sh, nil, false
	}
	if !sh.Enabled {
		httpErr(w, http.StatusServiceUnavailable, "this share is disabled")
		return sh, nil, false
	}
	if write {
		if sh.ReadOnly {
			httpErr(w, http.StatusForbidden, "this share is read-only")
			return sh, nil, false
		}
		if !p.User.CanWrite() {
			httpErr(w, http.StatusForbidden, "your account has read-only access")
			return sh, nil, false
		}
	}
	root, err := fsx.Open(sh.Path)
	if err != nil {
		s.fsErr(w, err)
		return sh, nil, false
	}
	return sh, root, true
}

// fsErr maps filesystem errors to HTTP responses without leaking server paths.
func (s *Server) fsErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, fsx.ErrUnsafe):
		httpErr(w, http.StatusBadRequest, "invalid or unsafe path")
	case errors.Is(err, fsx.ErrNotFound):
		httpErr(w, http.StatusNotFound, "not found")
	case errors.Is(err, fsx.ErrExists):
		httpErr(w, http.StatusConflict, "an item with that name already exists")
	case errors.Is(err, fsx.ErrNotDir):
		httpErr(w, http.StatusBadRequest, "not a folder")
	case errors.Is(err, fsx.ErrIsDir):
		httpErr(w, http.StatusBadRequest, "that is a folder")
	case errors.Is(err, fsx.ErrTooBig):
		httpErr(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, fsx.ErrNoSpace):
		s.log.Error("disk full")
		httpErr(w, http.StatusInsufficientStorage, "the server disk is full")
	case errors.Is(err, fsx.ErrUnavailable):
		s.log.Warn("share folder unavailable", "err", err)
		httpErr(w, http.StatusServiceUnavailable, "this share's folder is currently unavailable (missing or drive disconnected)")
	case errors.Is(err, fsx.ErrDenied):
		s.log.Warn("filesystem permission denied", "err", err)
		httpErr(w, http.StatusForbidden, "the HomeServer service account is not allowed to access that item")
	default:
		s.log.Error("filesystem error", "err", err)
		httpErr(w, http.StatusInternalServerError, "internal error")
	}
}

func (s *Server) acquire(w http.ResponseWriter) bool {
	select {
	case s.sem <- struct{}{}:
		s.active.Add(1)
		return true
	case <-time.After(2 * time.Second):
		w.Header().Set("Retry-After", "5")
		httpErr(w, http.StatusServiceUnavailable, "the server is busy with other transfers; try again shortly")
		return false
	}
}

func (s *Server) release() {
	<-s.sem
	s.active.Add(-1)
}

func (s *Server) record(r *http.Request, p *principal, sh store.Share, rel, dir string, n int64, status string) {
	if err := s.st.AddTransfer(store.Transfer{User: p.User.Username, Share: sh.Name, Path: rel, Direction: dir, Bytes: n, Status: status, IP: clientIP(r)}); err != nil {
		s.log.Error("record transfer", "err", err)
	}
	s.log.Info("transfer", "dir", dir, "share", sh.Name, "path", rel, "bytes", n, "status", status, "user", p.User.Username)
}

func (s *Server) handleList(w http.ResponseWriter, r *http.Request, p *principal) {
	sh, root, ok := s.openShare(w, r, p, false)
	if !ok {
		return
	}
	defer root.Close()
	q := r.URL.Query()
	sortKey := q.Get("sort")
	switch sortKey {
	case "name", "size", "type", "modified":
	default:
		sortKey = "name"
	}
	off, _ := strconv.Atoi(q.Get("offset"))
	lim, _ := strconv.Atoi(q.Get("limit"))
	rel := q.Get("path")
	res, err := root.List(rel, fsx.ListOptions{Sort: sortKey, Desc: q.Get("desc") == "1", Query: q.Get("q"), Offset: off, Limit: lim})
	if err != nil {
		s.fsErr(w, err)
		return
	}
	if lim <= 0 || lim > 1000 {
		lim = 200
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": strings.Trim(rel, "/"), "entries": res.Entries, "total": res.Total, "offset": off, "limit": lim,
		"canWrite": !sh.ReadOnly && p.User.CanWrite(),
	})
}

// streamWriter counts bytes and keeps a rolling write deadline so a stalled
// client frees its slot while slow-but-moving transfers continue.
type streamWriter struct {
	http.ResponseWriter
	rc *http.ResponseController
	n  int64
}

func (w *streamWriter) Write(b []byte) (int, error) {
	_ = w.rc.SetWriteDeadline(time.Now().Add(idleTimeout))
	n, err := w.ResponseWriter.Write(b)
	w.n += int64(n)
	return n, err
}
func (w *streamWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type idleReader struct {
	r  io.Reader
	rc *http.ResponseController
}

func (i idleReader) Read(b []byte) (int, error) {
	_ = i.rc.SetReadDeadline(time.Now().Add(idleTimeout))
	return i.r.Read(b)
}

var inlineTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"video/mp4": true, "video/webm": true, "audio/mpeg": true, "audio/ogg": true, "audio/wav": true,
	"audio/mp4": true, "application/pdf": true, "text/plain": true,
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request, p *principal) {
	sh, root, ok := s.openShare(w, r, p, false)
	if !ok {
		return
	}
	defer root.Close()
	rel := r.URL.Query().Get("path")
	f, fi, err := root.OpenFile(rel)
	if err != nil {
		s.fsErr(w, err)
		return
	}
	defer f.Close()
	if !s.acquire(w) {
		return
	}
	defer s.release()

	name := filepath.Base(filepath.FromSlash(rel))
	ctype := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	base, _, _ := mime.ParseMediaType(ctype)
	disp := "attachment"
	if r.URL.Query().Get("inline") == "1" && inlineTypes[base] {
		disp = "inline"
	}
	h := w.Header()
	h.Set("Content-Type", ctype)
	h.Set("Content-Disposition", mime.FormatMediaType(disp, map[string]string{"filename": name}))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'; media-src 'self'; img-src 'self'; style-src 'unsafe-inline'")
	h.Set("Accept-Ranges", "bytes")
	rc := http.NewResponseController(w)
	defer rc.SetWriteDeadline(time.Time{})
	sw := &streamWriter{ResponseWriter: w, rc: rc}
	http.ServeContent(sw, r, name, fi.ModTime(), f)
	status := "ok"
	if r.Context().Err() != nil {
		status = "aborted"
	}
	if r.Header.Get("Range") == "" || status == "aborted" || sw.n > 0 {
		s.record(r, p, sh, strings.Trim(rel, "/"), "download", sw.n, status)
	}
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request, p *principal) {
	sh, root, ok := s.openShare(w, r, p, true)
	if !ok {
		return
	}
	defer root.Close()
	q := r.URL.Query()
	dir, name, policy := q.Get("path"), q.Get("name"), q.Get("conflict")
	if policy == "" {
		policy = fsx.ConflictFail
	}
	if policy != fsx.ConflictFail && policy != fsx.ConflictRename && policy != fsx.ConflictReplace {
		httpErr(w, http.StatusBadRequest, "invalid conflict policy")
		return
	}
	max := s.settings().MaxUploadBytes
	if max > 0 && r.ContentLength > max {
		httpErr(w, http.StatusRequestEntityTooLarge, "file exceeds the upload limit of "+strconv.FormatInt(max, 10)+" bytes")
		return
	}
	s.maybeCleanTemp(sh, root)
	if !s.acquire(w) {
		return
	}
	defer s.release()
	up, err := root.BeginUpload(dir, name)
	if err != nil {
		s.fsErr(w, err)
		return
	}
	rc := http.NewResponseController(w)
	defer rc.SetReadDeadline(time.Time{})
	var body io.Reader = r.Body
	if max > 0 {
		body = http.MaxBytesReader(w, r.Body, max)
	}
	body = idleReader{r: body, rc: rc}
	bp := s.bufPool.Get().(*[]byte)
	n, err := io.CopyBuffer(struct{ io.Writer }{up.File}, struct{ io.Reader }{body}, *bp)
	s.bufPool.Put(bp)
	rel := strings.Trim(strings.Trim(dir, "/")+"/"+name, "/")
	if err != nil {
		up.Abort()
		var mbe *http.MaxBytesError
		switch {
		case errors.As(err, &mbe):
			s.record(r, p, sh, rel, "upload", n, "error")
			httpErr(w, http.StatusRequestEntityTooLarge, "file exceeds the upload limit")
		case errors.Is(err, fsx.ErrNoSpace) || isDiskFull(err):
			s.record(r, p, sh, rel, "upload", n, "error")
			s.fsErr(w, fsx.ErrNoSpace)
		default:
			s.record(r, p, sh, rel, "upload", n, "aborted") // client went away mid-transfer
			s.log.Warn("upload interrupted", "share", sh.Name, "path", rel, "bytes", n, "err", err)
		}
		return
	}
	final, err := up.Commit(policy)
	if err != nil {
		s.record(r, p, sh, rel, "upload", n, "error")
		s.fsErr(w, err)
		return
	}
	s.record(r, p, sh, strings.Trim(strings.Trim(dir, "/")+"/"+final, "/"), "upload", n, "ok")
	writeJSON(w, http.StatusCreated, map[string]any{"name": final, "size": n})
}

func isDiskFull(err error) bool {
	e := fsx.MapErr(err)
	return errors.Is(e, fsx.ErrNoSpace)
}

// sweepTemp removes abandoned partial uploads (only the per-share temp dir).
func (s *Server) sweepTemp() {
	shares, err := s.st.ListShares()
	if err != nil {
		return
	}
	for _, sh := range shares {
		root, err := fsx.Open(sh.Path)
		if err != nil {
			s.log.Warn("share unavailable at startup", "share", sh.Name)
			continue
		}
		if n := root.CleanTemp(time.Hour); n > 0 {
			s.log.Info("removed abandoned partial uploads", "share", sh.Name, "count", n)
		}
		root.Close()
	}
}

func (s *Server) maybeCleanTemp(sh store.Share, root *fsx.Root) {
	if t, ok := s.tempCleaned.Load(sh.ID); ok && time.Since(t.(time.Time)) < time.Hour {
		return
	}
	s.tempCleaned.Store(sh.ID, time.Now())
	root.CleanTemp(6 * time.Hour)
}

type pathBody struct {
	Path string `json:"path"`
	Name string `json:"name"`
	Dest string `json:"dest"`
}

func (s *Server) mutate(w http.ResponseWriter, r *http.Request, p *principal, event string, op func(root *fsx.Root, b pathBody) error) {
	sh, root, ok := s.openShare(w, r, p, true)
	if !ok {
		return
	}
	defer root.Close()
	var b pathBody
	if !decode(w, r, &b) {
		return
	}
	if err := op(root, b); err != nil {
		s.fsErr(w, err)
		return
	}
	s.audit(r, p.User.Username, event, sh.Name+": "+strings.Trim(b.Path, "/")+" "+b.Name+b.Dest)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMkdir(w http.ResponseWriter, r *http.Request, p *principal) {
	s.mutate(w, r, p, "file.mkdir", func(root *fsx.Root, b pathBody) error { return root.Mkdir(b.Path, b.Name) })
}

func (s *Server) handleRename(w http.ResponseWriter, r *http.Request, p *principal) {
	s.mutate(w, r, p, "file.rename", func(root *fsx.Root, b pathBody) error { return root.Rename(b.Path, b.Name) })
}

func (s *Server) handleMove(w http.ResponseWriter, r *http.Request, p *principal) {
	s.mutate(w, r, p, "file.move", func(root *fsx.Root, b pathBody) error { return root.Move(b.Path, b.Dest) })
}

func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request, p *principal) {
	s.mutate(w, r, p, "file.delete", func(root *fsx.Root, b pathBody) error { return root.Remove(b.Path) })
}

// Package server implements the HomeServer HTTP API and static web UI.
package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/abhijithkm/homeserver/internal/auth"
	"github.com/abhijithkm/homeserver/internal/netinfo"
	"github.com/abhijithkm/homeserver/internal/paths"
	"github.com/abhijithkm/homeserver/internal/store"
	"github.com/abhijithkm/homeserver/internal/tlsutil"
	"github.com/abhijithkm/homeserver/internal/update"
)

// ErrRestart is returned by Run when an administrator requested a restart.
var ErrRestart = errors.New("restart requested")

const (
	cookieName  = "hs_session"
	idleTimeout = 2 * time.Minute // max time a transfer may stall before the connection is dropped
	maxJSONBody = 64 << 10
)

// Config carries the server's dependencies.
type Config struct {
	Store   *store.Store
	Dirs    paths.Dirs
	Version string
	Log     *slog.Logger
	Web     fs.FS          // compiled frontend (index.html at root); may be nil
	Updater *update.Client // release lookups; nil uses GitHub
}

// Server is the HomeServer HTTP server.
type Server struct {
	cfg     Config
	st      *store.Store
	log     *slog.Logger
	setMu   sync.RWMutex
	set     store.Settings
	limiter *auth.Limiter
	sem     chan struct{}
	active  atomic.Int64
	started time.Time
	tlsOn   bool
	listen  atomic.Value // string

	restart     chan struct{}
	restartOnce sync.Once
	bufPool     sync.Pool
	tempCleaned sync.Map
	handler     http.Handler

	upMu   sync.Mutex
	upInfo update.Info
	upErr  string
	upAt   time.Time
}

// New builds a server from persisted settings.
func New(cfg Config) (*Server, error) {
	set, err := cfg.Store.GetSettings()
	if err != nil {
		return nil, err
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Server{
		cfg: cfg, st: cfg.Store, log: cfg.Log, set: set,
		limiter: auth.NewLimiter(5, 15*time.Minute, 15*time.Minute),
		sem:     make(chan struct{}, set.MaxTransfers),
		restart: make(chan struct{}),
		bufPool: sync.Pool{New: func() any { b := make([]byte, 64<<10); return &b }},
	}
	s.handler = s.routes()
	return s, nil
}

// Handler exposes the router (used by tests).
func (s *Server) Handler() http.Handler { return s.handler }

// RequestRestart asks Run to return ErrRestart after a graceful shutdown.
func (s *Server) RequestRestart() { s.restartOnce.Do(func() { close(s.restart) }) }

func (s *Server) settings() store.Settings {
	s.setMu.RLock()
	defer s.setMu.RUnlock()
	return s.set
}

// URLs returns the access URLs for this server.
func (s *Server) URLs() []string {
	set := s.settings()
	scheme := "http"
	if s.tlsOn {
		scheme = "https"
	}
	var out []string
	for _, a := range netinfo.LocalIPv4() {
		out = append(out, fmt.Sprintf("%s://%s", scheme, net.JoinHostPort(a.IP, strconv.Itoa(set.Port))))
	}
	if len(out) == 0 {
		out = append(out, fmt.Sprintf("%s://%s", scheme, net.JoinHostPort("localhost", strconv.Itoa(set.Port))))
	}
	return out
}

// Run listens and serves until ctx is cancelled or a restart is requested.
func (s *Server) Run(ctx context.Context) error {
	set := s.settings()
	addr := net.JoinHostPort(set.BindAddr, strconv.Itoa(set.Port))
	srv := &http.Server{
		Handler:           s.handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    32 << 10,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
	}
	var ips []string
	for _, a := range netinfo.LocalIPv4() {
		ips = append(ips, a.IP)
	}
	switch set.TLSMode {
	case "selfsigned":
		c, err := tlsutil.SelfSigned(s.cfg.Dirs.TLS, ips)
		if err != nil {
			return fmt.Errorf("tls: %w", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{c}, MinVersion: tls.VersionTLS12}
		s.tlsOn = true
	case "custom":
		c, err := tls.LoadX509KeyPair(set.TLSCert, set.TLSKey)
		if err != nil {
			return fmt.Errorf("tls: cannot load certificate: %w", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{c}, MinVersion: tls.VersionTLS12}
		s.tlsOn = true
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		if strings.Contains(err.Error(), "Only one usage") || strings.Contains(err.Error(), "address already in use") {
			return fmt.Errorf("port %d is already in use by another program; stop it or choose another port (homeserver set-port <n>): %w", set.Port, err)
		}
		return fmt.Errorf("cannot listen on %s: %w", addr, err)
	}
	s.listen.Store(ln.Addr().String())
	s.started = time.Now()
	s.sweepTemp()
	s.writeRuntime()
	s.log.Info("listening", "addr", ln.Addr().String(), "tls", s.tlsOn, "version", s.cfg.Version)

	errc := make(chan error, 1)
	go func() {
		if s.tlsOn {
			errc <- srv.ServeTLS(ln, "", "")
		} else {
			errc <- srv.Serve(ln)
		}
	}()
	var reason error
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	case <-s.restart:
		reason = ErrRestart
	}
	s.log.Info("shutting down", "restart", reason != nil)
	sctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		s.log.Warn("graceful shutdown timed out; closing remaining connections", "err", err)
		srv.Close()
	}
	_ = os.Remove(s.cfg.Dirs.Runtime)
	return reason
}

func (s *Server) writeRuntime() {
	set := s.settings()
	scheme := "http"
	if s.tlsOn {
		scheme = "https"
	}
	b, _ := json.Marshal(map[string]any{
		"pid": os.Getpid(), "port": set.Port, "scheme": scheme, "urls": s.URLs(),
		"version": s.cfg.Version, "started": s.started.Unix(),
	})
	if err := os.WriteFile(s.cfg.Dirs.Runtime, b, 0o644); err != nil {
		s.log.Warn("cannot write runtime info", "err", err)
	}
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		httpErr(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	return true
}

func clientIP(r *http.Request) string {
	h, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return h
}

func isLoopback(r *http.Request) bool {
	ip := net.ParseIP(clientIP(r))
	return ip != nil && ip.IsLoopback()
}

// sameOrigin rejects cross-site requests: an Origin header, when present, must
// match the Host the request was addressed to.
func sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && strings.EqualFold(u.Host, r.Host)
	}
	if sf := r.Header.Get("Sec-Fetch-Site"); sf != "" && sf != "same-origin" && sf != "none" {
		return false
	}
	return true
}

func (s *Server) audit(r *http.Request, actor, event, detail string) {
	s.log.Info("audit", "event", event, "actor", actor, "ip", clientIP(r), "detail", detail)
	if err := s.st.AddAudit(actor, clientIP(r), event, detail); err != nil {
		s.log.Error("audit write failed", "err", err)
	}
}

func within(base, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(base), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// ---- routing ----

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /api/setup-status", s.handleSetupStatus)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.authed(false, s.handleLogout))
	mux.HandleFunc("GET /api/session", s.authed(false, s.handleSession))
	mux.HandleFunc("POST /api/password", s.authed(false, s.handleChangePassword))
	mux.HandleFunc("GET /api/network", s.authed(false, s.handleNetwork))
	mux.HandleFunc("GET /api/qr.png", s.authed(false, s.handleQR))

	mux.HandleFunc("GET /api/shares", s.authed(false, s.handleShares))
	mux.HandleFunc("POST /api/shares", s.authed(true, s.handleShareAdd))
	mux.HandleFunc("PATCH /api/shares/{id}", s.authed(true, s.handleShareUpdate))
	mux.HandleFunc("DELETE /api/shares/{id}", s.authed(true, s.handleShareDelete))

	mux.HandleFunc("GET /api/shares/{id}/list", s.authed(false, s.handleList))
	mux.HandleFunc("GET /api/shares/{id}/download", s.authed(false, s.handleDownload))
	mux.HandleFunc("POST /api/shares/{id}/upload", s.authed(false, s.handleUpload))
	mux.HandleFunc("POST /api/shares/{id}/mkdir", s.authed(false, s.handleMkdir))
	mux.HandleFunc("POST /api/shares/{id}/rename", s.authed(false, s.handleRename))
	mux.HandleFunc("POST /api/shares/{id}/move", s.authed(false, s.handleMove))
	mux.HandleFunc("POST /api/shares/{id}/delete", s.authed(false, s.handleDelete))

	mux.HandleFunc("GET /api/admin/status", s.authed(true, s.handleStatus))
	mux.HandleFunc("GET /api/admin/diagnose", s.authed(true, s.handleDiagnose))
	mux.HandleFunc("GET /api/admin/settings", s.authed(true, s.handleSettingsGet))
	mux.HandleFunc("PUT /api/admin/settings", s.authed(true, s.handleSettingsPut))
	mux.HandleFunc("GET /api/admin/users", s.authed(true, s.handleUsers))
	mux.HandleFunc("POST /api/admin/users", s.authed(true, s.handleUserAdd))
	mux.HandleFunc("PATCH /api/admin/users/{id}", s.authed(true, s.handleUserUpdate))
	mux.HandleFunc("DELETE /api/admin/users/{id}", s.authed(true, s.handleUserDelete))
	mux.HandleFunc("POST /api/admin/users/{id}/password", s.authed(true, s.handleUserPassword))
	mux.HandleFunc("GET /api/admin/audit", s.authed(true, s.handleAudit))
	mux.HandleFunc("GET /api/admin/transfers", s.authed(true, s.handleTransfers))
	mux.HandleFunc("GET /api/admin/logs", s.authed(true, s.handleLogs))
	mux.HandleFunc("GET /api/admin/backup", s.authed(true, s.handleBackup))
	mux.HandleFunc("POST /api/admin/restart", s.authed(true, s.handleRestart))
	mux.HandleFunc("GET /api/admin/update", s.authed(true, s.handleUpdate))
	mux.HandleFunc("GET /api/admin/browse", s.authed(true, s.handleBrowse))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { httpErr(w, http.StatusNotFound, "not found") })
	mux.HandleFunc("/", s.handleStatic)

	return s.secure(mux)
}

func (s *Server) secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; media-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		if s.tlsOn {
			h.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		if r.Method == http.MethodOptions { // no CORS: cross-origin pre-flights are refused
			httpErr(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type principal struct {
	User  store.User
	CSRF  string
	Token string
}

func (s *Server) authed(admin bool, h func(http.ResponseWriter, *http.Request, *principal)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(cookieName)
		if err != nil || c.Value == "" {
			httpErr(w, http.StatusUnauthorized, "login required")
			return
		}
		ss, u, err := s.st.LookupSession(c.Value)
		if err != nil {
			httpErr(w, http.StatusUnauthorized, "login required")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if !sameOrigin(r) || r.Header.Get("X-CSRF-Token") != ss.CSRF {
				s.audit(r, u.Username, "csrf.reject", r.Method+" "+r.URL.Path)
				httpErr(w, http.StatusForbidden, "request rejected (CSRF check failed)")
				return
			}
		}
		if admin && !u.IsAdmin() {
			httpErr(w, http.StatusForbidden, "administrator access required")
			return
		}
		h(w, r, &principal{User: u, CSRF: ss.CSRF, Token: c.Value})
	}
}

// ---- static UI ----

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.cfg.Web == nil {
		http.Error(w, "web UI not built into this binary", http.StatusNotFound)
		return
	}
	p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if p == "" {
		p = "index.html"
	}
	f, err := s.cfg.Web.Open(p)
	if err != nil {
		p = "index.html" // SPA fallback
		f, err = s.cfg.Web.Open(p)
		if err != nil {
			http.Error(w, "web UI not built into this binary", http.StatusNotFound)
			return
		}
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		http.NotFound(w, r)
		return
	}
	if strings.HasPrefix(p, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	if strings.HasSuffix(p, ".webmanifest") { // not in Go's built-in table, and Windows may not have it registered
		w.Header().Set("Content-Type", "application/manifest+json")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, p, fi.ModTime(), rs)
		return
	}
	http.NotFound(w, r)
}

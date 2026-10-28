package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abhijithkm/homeserver/internal/auth"
	"github.com/abhijithkm/homeserver/internal/store"
)

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.st.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "database error"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.cfg.Version})
}

func (s *Server) handleSetupStatus(w http.ResponseWriter, r *http.Request) {
	n, err := s.st.CountUsers()
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"needsSetup": n == 0, "local": isLoopback(r)})
}

type credentials struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// handleSetup creates the first administrator. It works only while no user
// exists and only from the server computer itself, so nobody on the LAN can
// claim an unconfigured server.
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		httpErr(w, http.StatusForbidden, "request rejected")
		return
	}
	n, err := s.st.CountUsers()
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	if n > 0 {
		httpErr(w, http.StatusConflict, "setup already completed")
		return
	}
	if !isLoopback(r) {
		httpErr(w, http.StatusForbidden, "first-run setup must be done in a browser on the server computer (http://localhost)")
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	c.Username = strings.TrimSpace(c.Username)
	if err := auth.ValidateUsername(c.Username); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidatePassword(c.Password); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h, err := auth.Hash(c.Password)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, err := s.st.AddUser(c.Username, h, "admin")
	if err != nil {
		httpErr(w, http.StatusConflict, "setup already completed")
		return
	}
	s.audit(r, u.Username, "setup.complete", "first administrator created")
	s.startSession(w, r, u)
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User) {
	ttl := time.Duration(s.settings().SessionHours) * time.Hour
	tok, csrf, err := s.st.CreateSession(u.ID, ttl)
	if err != nil {
		s.log.Error("create session", "err", err)
		httpErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: tok, Path: "/", MaxAge: int(ttl.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.tlsOn,
	})
	writeJSON(w, http.StatusOK, map[string]any{"user": u, "csrf": csrf})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		httpErr(w, http.StatusForbidden, "request rejected")
		return
	}
	var c credentials
	if !decode(w, r, &c) {
		return
	}
	ip := clientIP(r)
	keys := []string{"ip:" + ip, "user:" + strings.ToLower(c.Username)}
	for _, k := range keys {
		if ok, wait := s.limiter.Allow(k); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
			s.audit(r, c.Username, "login.throttled", "")
			httpErr(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
			return
		}
	}
	u, err := s.st.UserByName(c.Username)
	valid := false
	if err == nil && !u.Disabled && len(c.Password) <= auth.MaxPasswordLen {
		valid = auth.Verify(c.Password, u.PWHash)
	} else if errors.Is(err, store.ErrNotFound) || u.Disabled {
		auth.DummyVerify(c.Password)
	}
	if !valid {
		for _, k := range keys {
			s.limiter.Fail(k)
		}
		s.audit(r, c.Username, "login.fail", "")
		httpErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	for _, k := range keys {
		s.limiter.Reset(k)
	}
	s.audit(r, u.Username, "login.ok", "")
	s.startSession(w, r, u)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request, p *principal) {
	_ = s.st.DeleteSession(p.Token)
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: s.tlsOn})
	s.audit(r, p.User.Username, "logout", "")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request, p *principal) {
	writeJSON(w, http.StatusOK, map[string]any{"user": p.User, "csrf": p.CSRF, "version": s.cfg.Version, "tls": s.tlsOn})
}

func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request, p *principal) {
	var b struct{ Current, New string }
	if !decode(w, r, &b) {
		return
	}
	if !auth.Verify(b.Current, p.User.PWHash) {
		s.audit(r, p.User.Username, "password.change.fail", "wrong current password")
		httpErr(w, http.StatusForbidden, "current password is incorrect")
		return
	}
	if err := auth.ValidatePassword(b.New); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	h, err := auth.Hash(b.New)
	if err != nil || s.st.SetPassword(p.User.ID, h) != nil {
		httpErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, p.User.Username, "password.change", "")
	// SetPassword revoked every session including this one; issue a fresh one.
	s.startSession(w, r, p.User)
}

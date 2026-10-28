package server

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"

	"github.com/abhijithkm/homeserver/internal/auth"
	"github.com/abhijithkm/homeserver/internal/netinfo"
	"github.com/abhijithkm/homeserver/internal/store"
)

func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request, p *principal) {
	set := s.settings()
	writeJSON(w, http.StatusOK, map[string]any{
		"urls": s.URLs(), "addresses": netinfo.LocalIPv4(), "port": set.Port, "tls": s.tlsOn,
		"listening": s.listenAddr(),
	})
}

func (s *Server) listenAddr() string {
	v, _ := s.listen.Load().(string)
	return v
}

func (s *Server) handleQR(w http.ResponseWriter, r *http.Request, p *principal) {
	urls := s.URLs()
	i, _ := strconv.Atoi(r.URL.Query().Get("i"))
	if i < 0 || i >= len(urls) {
		i = 0
	}
	png, err := qrcode.Encode(urls[i], qrcode.Medium, 280)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "cannot render QR code")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request, p *principal) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	shares, _ := s.st.ListShares()
	offline := 0
	for _, sh := range shares {
		if st, _ := shareStatus(sh); st == "offline" || st == "denied" {
			offline++
		}
	}
	set := s.settings()
	writeJSON(w, http.StatusOK, map[string]any{
		"version": s.cfg.Version, "uptimeSeconds": int64(time.Since(s.started).Seconds()), "pid": os.Getpid(),
		"goroutines": runtime.NumGoroutine(), "heapBytes": m.HeapAlloc, "sysBytes": m.Sys,
		"activeTransfers": s.active.Load(), "maxTransfers": set.MaxTransfers,
		"listening": s.listenAddr(), "tls": s.tlsOn, "urls": s.URLs(),
		"schemaVersion": s.st.SchemaVersion(), "shares": len(shares), "sharesUnavailable": offline,
		"os": runtime.GOOS + "/" + runtime.GOARCH,
	})
}

func (s *Server) handleDiagnose(w http.ResponseWriter, r *http.Request, p *principal) {
	set := s.settings()
	type shareDiag struct {
		Name      string `json:"name"`
		Status    string `json:"status"`
		Problem   string `json:"problem,omitempty"`
		FreeBytes uint64 `json:"freeBytes,omitempty"`
		Total     uint64 `json:"totalBytes,omitempty"`
		LowDisk   bool   `json:"lowDisk,omitempty"`
	}
	shares, _ := s.st.ListShares()
	sd := []shareDiag{}
	for _, sh := range shares {
		st, pr := shareStatus(sh)
		d := shareDiag{Name: sh.Name, Status: st, Problem: pr}
		if st == "online" {
			if free, total, ok := diskFree(sh.Path); ok {
				d.FreeBytes, d.Total = free, total
				d.LowDisk = free < 1<<30 || (total > 0 && free*20 < total)
			}
		}
		sd = append(sd, d)
	}
	dbOK := s.st.Ping(r.Context()) == nil
	hints := []string{}
	if set.BindAddr == "127.0.0.1" || set.BindAddr == "localhost" {
		hints = append(hints, "The server only listens on this computer (bind address is loopback). Set bindAddr to 0.0.0.0 to allow other devices.")
	}
	if len(netinfo.LocalIPv4()) == 0 {
		hints = append(hints, "No active network connection with an IPv4 address was found.")
	}
	hints = append(hints,
		"If other devices cannot connect, allow the port through Windows Firewall for Private networks only:",
		fmt.Sprintf(`netsh advfirewall firewall add rule name="HomeServer" dir=in action=allow protocol=TCP localport=%d profile=private`, set.Port),
		"Make sure the Wi-Fi network is marked Private in Windows and that router client/AP isolation is off.")
	writeJSON(w, http.StatusOK, map[string]any{
		"database": dbOK, "listening": s.listenAddr(), "port": set.Port, "tls": s.tlsOn,
		"addresses": netinfo.LocalIPv4(), "urls": s.URLs(), "shares": sd, "hints": hints, "dataDir": s.cfg.Dirs.Data,
	})
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request, p *principal) {
	writeJSON(w, http.StatusOK, s.settings())
}

func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request, p *principal) {
	var n store.Settings
	if !decode(w, r, &n) {
		return
	}
	if err := n.Validate(); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	old := s.settings()
	if err := s.st.SaveSettings(n); err != nil {
		s.log.Error("save settings", "err", err)
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	s.setMu.Lock()
	s.set = n
	s.setMu.Unlock()
	needRestart := old.Port != n.Port || old.BindAddr != n.BindAddr || old.TLSMode != n.TLSMode ||
		old.TLSCert != n.TLSCert || old.TLSKey != n.TLSKey || old.MaxTransfers != n.MaxTransfers
	s.audit(r, p.User.Username, "settings.update", fmt.Sprintf("port=%d bind=%s tls=%s maxUpload=%d maxTransfers=%d session=%dh",
		n.Port, n.BindAddr, n.TLSMode, n.MaxUploadBytes, n.MaxTransfers, n.SessionHours))
	writeJSON(w, http.StatusOK, map[string]any{"settings": n, "restartRequired": needRestart})
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request, p *principal) {
	us, err := s.st.ListUsers()
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, us)
}

func validRole(r string) bool { return r == "admin" || r == "writer" || r == "reader" }

func (s *Server) handleUserAdd(w http.ResponseWriter, r *http.Request, p *principal) {
	var b struct{ Username, Password, Role string }
	if !decode(w, r, &b) {
		return
	}
	b.Username = strings.TrimSpace(b.Username)
	if err := auth.ValidateUsername(b.Username); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := auth.ValidatePassword(b.Password); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !validRole(b.Role) {
		httpErr(w, http.StatusBadRequest, "role must be admin, writer or reader")
		return
	}
	h, err := auth.Hash(b.Password)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	u, err := s.st.AddUser(b.Username, h, b.Role)
	if errors.Is(err, store.ErrConflict) {
		httpErr(w, http.StatusConflict, "that username is taken")
		return
	} else if err != nil {
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	s.audit(r, p.User.Username, "user.add", u.Username+" role="+u.Role)
	writeJSON(w, http.StatusCreated, u)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (s *Server) handleUserUpdate(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(r)
	if !ok {
		httpErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var b struct {
		Role     *string `json:"role"`
		Disabled *bool   `json:"disabled"`
	}
	if !decode(w, r, &b) {
		return
	}
	u, err := s.st.UserByID(id)
	if err != nil {
		httpErr(w, http.StatusNotFound, "user not found")
		return
	}
	if b.Role != nil {
		if !validRole(*b.Role) {
			httpErr(w, http.StatusBadRequest, "role must be admin, writer or reader")
			return
		}
		u.Role = *b.Role
	}
	if b.Disabled != nil {
		u.Disabled = *b.Disabled
	}
	if err := s.st.UpdateUser(id, u.Role, u.Disabled); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			httpErr(w, http.StatusConflict, err.Error())
			return
		}
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	s.audit(r, p.User.Username, "user.update", u.Username+" role="+u.Role+" disabled="+boolStr(u.Disabled))
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleUserDelete(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(r)
	if !ok {
		httpErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	u, err := s.st.UserByID(id)
	if err != nil {
		httpErr(w, http.StatusNotFound, "user not found")
		return
	}
	if err := s.st.DeleteUser(id); err != nil {
		if errors.Is(err, store.ErrLastAdmin) {
			httpErr(w, http.StatusConflict, err.Error())
			return
		}
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	s.audit(r, p.User.Username, "user.delete", u.Username)
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

func (s *Server) handleUserPassword(w http.ResponseWriter, r *http.Request, p *principal) {
	id, ok := pathID(r)
	if !ok {
		httpErr(w, http.StatusBadRequest, "invalid user id")
		return
	}
	var b struct{ Password string }
	if !decode(w, r, &b) {
		return
	}
	if err := auth.ValidatePassword(b.Password); err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	u, err := s.st.UserByID(id)
	if err != nil {
		httpErr(w, http.StatusNotFound, "user not found")
		return
	}
	h, err := auth.Hash(b.Password)
	if err != nil || s.st.SetPassword(id, h) != nil {
		httpErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.audit(r, p.User.Username, "user.password.reset", u.Username)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request, p *principal) {
	ev, err := s.st.RecentAudit(200)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, ev)
}

func (s *Server) handleTransfers(w http.ResponseWriter, r *http.Request, p *principal) {
	tr, err := s.st.RecentTransfers(100)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"active": s.active.Load(), "recent": tr})
}

// handleLogs returns the last N lines of the current log file. Secrets are
// never written to the log, so no redaction is needed here.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request, p *principal) {
	n, _ := strconv.Atoi(r.URL.Query().Get("lines"))
	if n <= 0 || n > 1000 {
		n = 200
	}
	f, err := os.Open(s.cfg.Dirs.LogFile)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"lines": []string{}})
		return
	}
	defer f.Close()
	ring := make([]string, 0, n)
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), 256<<10)
	for sc.Scan() {
		if len(ring) == n {
			copy(ring, ring[1:])
			ring = ring[:n-1]
		}
		ring = append(ring, sc.Text())
	}
	writeJSON(w, http.StatusOK, map[string]any{"lines": ring})
}

// handleBackup streams a consistent copy of the configuration database. It
// contains password hashes, so it is admin-only.
func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request, p *principal) {
	tmp := filepath.Join(s.cfg.Dirs.Data, fmt.Sprintf("backup-%d.tmp", time.Now().UnixNano()))
	if err := s.st.BackupTo(tmp); err != nil {
		s.log.Error("backup", "err", err)
		httpErr(w, http.StatusInternalServerError, "backup failed")
		return
	}
	defer os.Remove(tmp)
	f, err := os.Open(tmp)
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "backup failed")
		return
	}
	defer f.Close()
	s.audit(r, p.User.Username, "backup.download", "")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="homeserver-backup-`+time.Now().Format("20060102-150405")+`.db"`)
	_, _ = io.Copy(w, f)
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request, p *principal) {
	s.audit(r, p.User.Username, "server.restart", "")
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "restarting"})
	go func() {
		time.Sleep(300 * time.Millisecond) // let the response flush
		s.RequestRestart()
	}()
}

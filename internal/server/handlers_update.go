package server

import (
	"context"
	"net/http"
	"time"

	"github.com/abhijithkm/homeserver/internal/update"
)

const (
	updateCacheTTL     = 6 * time.Hour
	updateRefreshFloor = time.Minute
)

// handleUpdate reports whether a newer release exists. It only looks; installing
// is done by an administrator with `homeserver update` (UAC), never by the service.
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request, p *principal) {
	set := s.settings()
	if !set.UpdateCheck {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "current": s.cfg.Version})
		return
	}
	s.upMu.Lock()
	defer s.upMu.Unlock()
	age := time.Since(s.upAt)
	if s.upAt.IsZero() || age > updateCacheTTL || (r.URL.Query().Get("refresh") == "1" && age > updateRefreshFloor) {
		cl := s.cfg.Updater
		if cl == nil {
			cl = update.NewClient()
		}
		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		info, err := cl.Check(ctx, s.cfg.Version, set.UpdatePrerelease)
		cancel()
		s.upInfo, s.upAt, s.upErr = info, time.Now(), ""
		if err != nil {
			s.upErr = err.Error()
			s.log.Warn("update check", "err", err)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true, "info": s.upInfo, "error": s.upErr})
}

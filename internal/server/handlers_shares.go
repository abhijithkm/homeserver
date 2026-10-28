package server

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/abhijithkm/homeserver/internal/fsx"
	"github.com/abhijithkm/homeserver/internal/store"
)

type shareView struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	ReadOnly bool   `json:"readOnly"`
	Enabled  bool   `json:"enabled"`
	Status   string `json:"status"` // online | offline | denied | disabled
	Problem  string `json:"problem,omitempty"`
	CanWrite bool   `json:"canWrite"`
}

// shareStatus checks the folder right now; nothing is cached or polled.
func shareStatus(sh store.Share) (status, problem string) {
	if !sh.Enabled {
		return "disabled", ""
	}
	root, err := fsx.Open(sh.Path)
	if err == nil {
		root.Close()
		return "online", ""
	}
	if errors.Is(err, fsx.ErrDenied) {
		return "denied", "The HomeServer service account cannot read this folder. Grant it access (see docs/TROUBLESHOOTING.md)."
	}
	if _, serr := os.Stat(filepath.VolumeName(sh.Path) + `\`); serr != nil && filepath.VolumeName(sh.Path) != "" {
		return "offline", "Drive " + filepath.VolumeName(sh.Path) + " is not connected."
	}
	return "offline", "The folder is missing or not reachable."
}

func (s *Server) view(sh store.Share, p *principal) shareView {
	st, pr := shareStatus(sh)
	v := shareView{ID: sh.ID, Name: sh.Name, ReadOnly: sh.ReadOnly, Enabled: sh.Enabled, Status: st,
		CanWrite: !sh.ReadOnly && p.User.CanWrite() && st == "online"}
	if p.User.IsAdmin() {
		v.Path, v.Problem = sh.Path, pr
	}
	return v
}

func (s *Server) handleShares(w http.ResponseWriter, r *http.Request, p *principal) {
	shares, err := s.st.ListShares()
	if err != nil {
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	out := []shareView{}
	for _, sh := range shares {
		if !sh.Enabled && !p.User.IsAdmin() {
			continue
		}
		out = append(out, s.view(sh, p))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleShareAdd(w http.ResponseWriter, r *http.Request, p *principal) {
	var b struct {
		Name     string `json:"name"`
		Path     string `json:"path"`
		ReadOnly *bool  `json:"readOnly"`
	}
	if !decode(w, r, &b) {
		return
	}
	if !store.ValidShareName(b.Name) {
		httpErr(w, http.StatusBadRequest, "share name must be 1-64 characters without control characters")
		return
	}
	abs, err := fsx.ValidateRootPath(strings.TrimSpace(b.Path))
	if err != nil {
		httpErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if within(s.cfg.Dirs.Data, abs) || within(abs, s.cfg.Dirs.Data) {
		httpErr(w, http.StatusBadRequest, "this folder contains or lies inside HomeServer's own data folder and cannot be shared")
		return
	}
	existing, _ := s.st.ListShares()
	for _, e := range existing {
		if strings.EqualFold(filepath.Clean(e.Path), filepath.Clean(abs)) {
			httpErr(w, http.StatusConflict, "this folder is already shared as \""+e.Name+"\"")
			return
		}
	}
	ro := true // safe default: read-only
	if b.ReadOnly != nil {
		ro = *b.ReadOnly
	}
	sh, err := s.st.AddShare(b.Name, abs, ro)
	if errors.Is(err, store.ErrConflict) {
		httpErr(w, http.StatusConflict, "a share with this name already exists")
		return
	} else if err != nil {
		s.log.Error("add share", "err", err)
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	s.audit(r, p.User.Username, "share.add", sh.Name+" -> "+sh.Path+" readOnly="+boolStr(ro))
	writeJSON(w, http.StatusCreated, s.view(sh, p))
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (s *Server) handleShareUpdate(w http.ResponseWriter, r *http.Request, p *principal) {
	var b struct {
		Name     *string `json:"name"`
		ReadOnly *bool   `json:"readOnly"`
		Enabled  *bool   `json:"enabled"`
	}
	if !decode(w, r, &b) {
		return
	}
	if b.Name != nil && !store.ValidShareName(*b.Name) {
		httpErr(w, http.StatusBadRequest, "share name must be 1-64 characters without control characters")
		return
	}
	sh, err := s.st.UpdateShare(r.PathValue("id"), store.ShareUpdate{Name: b.Name, ReadOnly: b.ReadOnly, Enabled: b.Enabled})
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpErr(w, http.StatusNotFound, "share not found")
		return
	case errors.Is(err, store.ErrConflict):
		httpErr(w, http.StatusConflict, "a share with this name already exists")
		return
	case err != nil:
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	s.audit(r, p.User.Username, "share.update", sh.Name+" readOnly="+boolStr(sh.ReadOnly)+" enabled="+boolStr(sh.Enabled))
	writeJSON(w, http.StatusOK, s.view(sh, p))
}

func (s *Server) handleShareDelete(w http.ResponseWriter, r *http.Request, p *principal) {
	sh, err := s.st.GetShare(r.PathValue("id"))
	if err != nil {
		httpErr(w, http.StatusNotFound, "share not found")
		return
	}
	if err := s.st.DeleteShare(sh.ID); err != nil {
		httpErr(w, http.StatusInternalServerError, "database error")
		return
	}
	s.audit(r, p.User.Username, "share.remove", sh.Name+" (files untouched)")
	writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
}

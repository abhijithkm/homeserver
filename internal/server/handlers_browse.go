package server

import (
	"errors"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// maxBrowseEntries bounds one folder listing; the picker is for choosing a
// share root, not for exploring huge trees.
const maxBrowseEntries = 1000

type browseEntry struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Readable bool   `json:"readable"` // the service account can open it
}

type browseResult struct {
	Path      string        `json:"path"`   // "" = the list of drives
	Parent    string        `json:"parent"` // "" at the top (drives list)
	Entries   []browseEntry `json:"entries"`
	Truncated bool          `json:"truncated,omitempty"`
}

// readableDir reports whether the service account can list the folder.
func readableDir(path string) bool {
	d, err := os.Open(path)
	if err != nil {
		return false
	}
	defer d.Close()
	_, err = d.ReadDir(1)
	return err == nil || errors.Is(err, io.EOF)
}

// handleBrowse lists the sub-folders of a folder on the server so an
// administrator can pick a share root instead of typing a path. Only folders
// are listed; links, junctions and hidden/system folders are left out because
// shares never follow or expose them.
func (s *Server) handleBrowse(w http.ResponseWriter, r *http.Request, p *principal) {
	raw := strings.TrimSpace(r.URL.Query().Get("path"))
	if raw == "" {
		writeJSON(w, http.StatusOK, browseResult{Entries: listRoots()})
		return
	}
	if !filepath.IsAbs(raw) {
		httpErr(w, http.StatusBadRequest, "path must be absolute")
		return
	}
	abs := filepath.Clean(raw)
	ents, err := os.ReadDir(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		httpErr(w, http.StatusNotFound, "folder does not exist")
		return
	case errors.Is(err, fs.ErrPermission):
		httpErr(w, http.StatusForbidden, "The HomeServer service account cannot read this folder. Grant it access (see docs/TROUBLESHOOTING.md).")
		return
	case err != nil:
		httpErr(w, http.StatusBadRequest, "cannot list this folder")
		return
	}
	res := browseResult{Path: abs, Entries: []browseEntry{}}
	if parent := filepath.Dir(abs); parent != abs {
		res.Parent = parent
	}
	for _, e := range ents {
		if !e.IsDir() || e.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			continue
		}
		if fi, err := e.Info(); err == nil && hiddenDir(fi) {
			continue
		}
		if len(res.Entries) >= maxBrowseEntries {
			res.Truncated = true
			break
		}
		child := filepath.Join(abs, e.Name())
		res.Entries = append(res.Entries, browseEntry{Name: e.Name(), Path: child, Readable: readableDir(child)})
	}
	sort.Slice(res.Entries, func(i, j int) bool {
		return strings.ToLower(res.Entries[i].Name) < strings.ToLower(res.Entries[j].Name)
	})
	writeJSON(w, http.StatusOK, res)
}

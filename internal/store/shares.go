package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

// Share is a registered folder.
type Share struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path,omitempty"`
	ReadOnly  bool   `json:"readOnly"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"createdAt"`
}

var (
	// ErrNotFound is returned when a row does not exist.
	ErrNotFound = errors.New("not found")
	// ErrConflict is returned on a duplicate unique value.
	ErrConflict = errors.New("already exists")
	shareIDRe   = regexp.MustCompile(`^[0-9a-f]{16}$`)
)

// ValidShareID reports whether id has the expected shape.
func ValidShareID(id string) bool { return shareIDRe.MatchString(id) }

// ValidShareName checks a display name.
func ValidShareName(n string) bool {
	n = strings.TrimSpace(n)
	if n == "" || len(n) > 64 {
		return false
	}
	for _, r := range n {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// AddShare registers a folder.
func (s *Store) AddShare(name, path string, readOnly bool) (Share, error) {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return Share{}, err
	}
	sh := Share{ID: hex.EncodeToString(raw[:]), Name: strings.TrimSpace(name), Path: path, ReadOnly: readOnly, Enabled: true, CreatedAt: now()}
	_, err := s.db.Exec("INSERT INTO shares(id,name,path,read_only,enabled,created_at) VALUES(?,?,?,?,1,?)",
		sh.ID, sh.Name, sh.Path, readOnly, sh.CreatedAt)
	if isUnique(err) {
		return Share{}, ErrConflict
	}
	return sh, err
}

func scanShare(r interface{ Scan(...any) error }) (Share, error) {
	var sh Share
	err := r.Scan(&sh.ID, &sh.Name, &sh.Path, &sh.ReadOnly, &sh.Enabled, &sh.CreatedAt)
	return sh, err
}

// ListShares returns all shares ordered by name.
func (s *Store) ListShares() ([]Share, error) {
	rows, err := s.db.Query("SELECT id,name,path,read_only,enabled,created_at FROM shares ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Share
	for rows.Next() {
		sh, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sh)
	}
	return out, rows.Err()
}

// GetShare loads one share.
func (s *Store) GetShare(id string) (Share, error) {
	if !ValidShareID(id) {
		return Share{}, ErrNotFound
	}
	sh, err := scanShare(s.db.QueryRow("SELECT id,name,path,read_only,enabled,created_at FROM shares WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Share{}, ErrNotFound
	}
	return sh, err
}

// ShareUpdate holds optional changes.
type ShareUpdate struct {
	Name     *string
	ReadOnly *bool
	Enabled  *bool
}

// UpdateShare applies the non-nil fields transactionally.
func (s *Store) UpdateShare(id string, u ShareUpdate) (Share, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Share{}, err
	}
	defer tx.Rollback()
	sh, err := scanShare(tx.QueryRow("SELECT id,name,path,read_only,enabled,created_at FROM shares WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return Share{}, ErrNotFound
	} else if err != nil {
		return Share{}, err
	}
	if u.Name != nil {
		sh.Name = strings.TrimSpace(*u.Name)
	}
	if u.ReadOnly != nil {
		sh.ReadOnly = *u.ReadOnly
	}
	if u.Enabled != nil {
		sh.Enabled = *u.Enabled
	}
	if _, err := tx.Exec("UPDATE shares SET name=?, read_only=?, enabled=? WHERE id=?", sh.Name, sh.ReadOnly, sh.Enabled, id); err != nil {
		if isUnique(err) {
			return Share{}, ErrConflict
		}
		return Share{}, err
	}
	return sh, tx.Commit()
}

// DeleteShare unregisters a share. Files are never touched.
func (s *Store) DeleteShare(id string) error {
	r, err := s.db.Exec("DELETE FROM shares WHERE id=?", id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

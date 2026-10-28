package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// User is an account.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Role      string `json:"role"` // admin | writer | reader
	Disabled  bool   `json:"disabled"`
	CreatedAt int64  `json:"createdAt"`
	PWHash    string `json:"-"`
}

// CanWrite reports whether the role may modify files (share mode still applies).
func (u User) CanWrite() bool { return u.Role == "admin" || u.Role == "writer" }

// IsAdmin reports admin role.
func (u User) IsAdmin() bool { return u.Role == "admin" }

// CountUsers returns number of users.
func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&n)
	return n, err
}

// AddUser inserts a user with an already-hashed password.
func (s *Store) AddUser(username, hash, role string) (User, error) {
	u := User{Username: username, Role: role, PWHash: hash, CreatedAt: now()}
	r, err := s.db.Exec("INSERT INTO users(username,pw_hash,role,created_at) VALUES(?,?,?,?)", username, hash, role, u.CreatedAt)
	if isUnique(err) {
		return User{}, ErrConflict
	}
	if err != nil {
		return User{}, err
	}
	u.ID, _ = r.LastInsertId()
	return u, nil
}

func scanUser(r interface{ Scan(...any) error }) (User, error) {
	var u User
	err := r.Scan(&u.ID, &u.Username, &u.PWHash, &u.Role, &u.Disabled, &u.CreatedAt)
	return u, err
}

const userCols = "id,username,pw_hash,role,disabled,created_at"

// UserByName loads by username (case-insensitive).
func (s *Store) UserByName(name string) (User, error) {
	u, err := scanUser(s.db.QueryRow("SELECT "+userCols+" FROM users WHERE username=?", name))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// UserByID loads by id.
func (s *Store) UserByID(id int64) (User, error) {
	u, err := scanUser(s.db.QueryRow("SELECT "+userCols+" FROM users WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrNotFound
	}
	return u, err
}

// ListUsers returns all users.
func (s *Store) ListUsers() ([]User, error) {
	rows, err := s.db.Query("SELECT " + userCols + " FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetPassword replaces a user's password hash and revokes their sessions.
func (s *Store) SetPassword(id int64, hash string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.Exec("UPDATE users SET pw_hash=? WHERE id=?", hash, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec("DELETE FROM sessions WHERE user_id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// ErrLastAdmin is returned when an operation would leave no active admin.
var ErrLastAdmin = errors.New("cannot remove or demote the last active admin")

// UpdateUser changes role / disabled. Refuses to remove the last active admin.
func (s *Store) UpdateUser(id int64, role string, disabled bool) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var curRole string
	var curDisabled bool
	if err := tx.QueryRow("SELECT role, disabled FROM users WHERE id=?", id).Scan(&curRole, &curDisabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if curRole == "admin" && !curDisabled && (role != "admin" || disabled) {
		var others int
		if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0 AND id<>?", id).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.Exec("UPDATE users SET role=?, disabled=? WHERE id=?", role, disabled, id); err != nil {
		return err
	}
	if disabled {
		if _, err := tx.Exec("DELETE FROM sessions WHERE user_id=?", id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// DeleteUser removes a user unless they are the last active admin.
func (s *Store) DeleteUser(id int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var curRole string
	var curDisabled bool
	if err := tx.QueryRow("SELECT role, disabled FROM users WHERE id=?", id).Scan(&curRole, &curDisabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if curRole == "admin" && !curDisabled {
		var others int
		if err := tx.QueryRow("SELECT COUNT(*) FROM users WHERE role='admin' AND disabled=0 AND id<>?", id).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.Exec("DELETE FROM users WHERE id=?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// Session is a login session.
type Session struct {
	UserID    int64
	CSRF      string
	ExpiresAt int64
}

func hashToken(t string) string {
	h := sha256.Sum256([]byte(t))
	return hex.EncodeToString(h[:])
}

func randToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateSession returns the raw cookie token (only its hash is stored).
func (s *Store) CreateSession(userID int64, ttl time.Duration) (token, csrf string, err error) {
	if token, err = randToken(32); err != nil {
		return
	}
	if csrf, err = randToken(16); err != nil {
		return
	}
	n := now()
	_, err = s.db.Exec("INSERT INTO sessions(token_hash,user_id,csrf,created_at,expires_at) VALUES(?,?,?,?,?)",
		hashToken(token), userID, csrf, n, n+int64(ttl.Seconds()))
	// opportunistic cleanup keeps the table small without a timer goroutine
	_, _ = s.db.Exec("DELETE FROM sessions WHERE expires_at < ?", n)
	return
}

// LookupSession validates a token; expired sessions are deleted.
func (s *Store) LookupSession(token string) (Session, User, error) {
	var ss Session
	err := s.db.QueryRow("SELECT user_id,csrf,expires_at FROM sessions WHERE token_hash=?", hashToken(token)).
		Scan(&ss.UserID, &ss.CSRF, &ss.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ss, User{}, ErrNotFound
	} else if err != nil {
		return ss, User{}, err
	}
	if ss.ExpiresAt <= now() {
		_ = s.DeleteSession(token)
		return ss, User{}, ErrNotFound
	}
	u, err := s.UserByID(ss.UserID)
	if err != nil || u.Disabled {
		return ss, User{}, ErrNotFound
	}
	return ss, u, nil
}

// DeleteSession logs out one token.
func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec("DELETE FROM sessions WHERE token_hash=?", hashToken(token))
	return err
}

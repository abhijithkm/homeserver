// Package store persists HomeServer configuration in SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite" // pure-Go SQLite driver (no CGO)
)

// ErrSchemaTooNew means the database was written by a newer HomeServer. It is
// never treated as corruption: the file is left untouched.
var ErrSchemaTooNew = errors.New("database schema is newer than this version of HomeServer (install the newer version)")

// Store wraps the SQLite database.
type Store struct {
	db   *sql.DB
	path string
}

var migrations = []string{
	`CREATE TABLE settings(key TEXT PRIMARY KEY, value TEXT NOT NULL);
	CREATE TABLE users(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT NOT NULL UNIQUE COLLATE NOCASE,
		pw_hash TEXT NOT NULL,
		role TEXT NOT NULL CHECK(role IN ('admin','writer','reader')),
		disabled INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL);
	CREATE TABLE shares(
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		path TEXT NOT NULL,
		read_only INTEGER NOT NULL DEFAULT 1,
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL);
	CREATE TABLE sessions(
		token_hash TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		csrf TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		expires_at INTEGER NOT NULL);
	CREATE INDEX sessions_expires ON sessions(expires_at);
	CREATE TABLE audit(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts INTEGER NOT NULL, actor TEXT NOT NULL, ip TEXT NOT NULL,
		event TEXT NOT NULL, detail TEXT NOT NULL);
	CREATE TABLE transfers(
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts INTEGER NOT NULL, username TEXT NOT NULL, share TEXT NOT NULL,
		path TEXT NOT NULL, direction TEXT NOT NULL, bytes INTEGER NOT NULL,
		status TEXT NOT NULL, ip TEXT NOT NULL);`,
}

const dsnPragmas = "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-2000)"

// Open opens (creating if needed) the database. A corrupt database is moved
// aside and replaced by a fresh one; recoveredFrom names the moved file
// (empty when no recovery happened).
func Open(path string) (s *Store, recoveredFrom string, err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", err
	}
	s, err = openAndCheck(path)
	if err == nil {
		return s, "", nil
	}
	if _, statErr := os.Stat(path); statErr != nil || errors.Is(err, ErrSchemaTooNew) {
		return nil, "", err // not a corruption problem
	}
	slog.Error("database failed integrity check; moving aside", "err", err)
	bad := fmt.Sprintf("%s.corrupt-%s", path, time.Now().Format("20060102-150405"))
	for _, suf := range []string{"", "-wal", "-shm"} {
		_ = os.Rename(path+suf, bad+suf)
	}
	s, err = openAndCheck(path)
	if err != nil {
		return nil, "", err
	}
	return s, bad, nil
}

func openAndCheck(path string) (*Store, error) {
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+dsnPragmas)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(time.Minute)
	s := &Store{db: db, path: path}
	var res string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&res); err != nil {
		db.Close()
		return nil, fmt.Errorf("quick_check: %w", err)
	}
	if res != "ok" {
		db.Close()
		return nil, fmt.Errorf("quick_check: %s", res)
	}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	_ = os.Chmod(path, 0o600)
	return s, nil
}

func (s *Store) migrate() error {
	var v int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	if v > len(migrations) {
		return fmt.Errorf("%w: database is version %d, this build understands %d", ErrSchemaTooNew, v, len(migrations))
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// SchemaVersion returns the current schema version.
func (s *Store) SchemaVersion() int {
	var v int
	_ = s.db.QueryRow("PRAGMA user_version").Scan(&v)
	return v
}

// Close checkpoints the WAL and closes the database.
func (s *Store) Close() error {
	_, _ = s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return s.db.Close()
}

// Ping verifies the database responds.
func (s *Store) Ping(ctx context.Context) error { return s.db.PingContext(ctx) }

// BackupTo writes a consistent copy of the database to dest (must not exist).
func (s *Store) BackupTo(dest string) error {
	if _, err := os.Stat(dest); err == nil {
		return errors.New("backup destination already exists")
	}
	_, err := s.db.Exec("VACUUM INTO ?", dest)
	if err == nil {
		_ = os.Chmod(dest, 0o600)
	}
	return err
}

func now() int64 { return time.Now().Unix() }

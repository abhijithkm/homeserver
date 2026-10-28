package store

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func open(t *testing.T) (*Store, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "hs.db")
	s, rec, err := Open(p)
	if err != nil || rec != "" {
		t.Fatalf("open: %v rec=%q", err, rec)
	}
	t.Cleanup(func() { s.Close() })
	return s, p
}

func TestMigrationFreshAndIdempotent(t *testing.T) {
	s, p := open(t)
	if s.SchemaVersion() != len(migrations) {
		t.Fatalf("schema version %d", s.SchemaVersion())
	}
	s.Close()
	s2, _, err := Open(p) // reopening must not re-run migrations
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	if s2.SchemaVersion() != len(migrations) {
		t.Fatal("version changed on reopen")
	}
}

func TestNewerSchemaRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hs.db")
	db, _ := sql.Open("sqlite", "file:"+filepath.ToSlash(p))
	db.Exec("PRAGMA user_version = 999")
	db.Exec("CREATE TABLE x(a)")
	db.Close()
	s, rec, err := Open(p)
	if err == nil {
		s.Close()
		t.Fatalf("newer schema accepted (recovered=%q)", rec)
	}
}

func TestCorruptDatabaseRecovery(t *testing.T) {
	p := filepath.Join(t.TempDir(), "hs.db")
	if err := os.WriteFile(p, []byte(strings.Repeat("this is not a sqlite database ", 500)), 0o600); err != nil {
		t.Fatal(err)
	}
	s, rec, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if rec == "" {
		t.Fatal("expected recovery to be reported")
	}
	if _, err := os.Stat(rec); err != nil {
		t.Fatal("corrupt file not preserved")
	}
	if _, err := s.AddShare("x", "C:/x", true); err != nil {
		t.Fatalf("fresh database unusable: %v", err)
	}
}

func TestSettingsRoundTripAndValidation(t *testing.T) {
	s, _ := open(t)
	def, _ := s.GetSettings()
	if def.Port != 8686 || def.BindAddr != "0.0.0.0" {
		t.Fatalf("defaults: %+v", def)
	}
	def.Port, def.MaxUploadBytes = 9000, 1<<30
	if err := s.SaveSettings(def); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSettings()
	if got.Port != 9000 || got.MaxUploadBytes != 1<<30 {
		t.Fatalf("round trip: %+v", got)
	}
	bad := got
	bad.Port = 0
	if s.SaveSettings(bad) == nil {
		t.Fatal("port 0 accepted")
	}
	bad = got
	bad.TLSMode = "custom"
	if s.SaveSettings(bad) == nil {
		t.Fatal("custom TLS without files accepted")
	}
}

func TestSharesCRUDAndUniqueness(t *testing.T) {
	s, _ := open(t)
	a, err := s.AddShare("Photos", `D:\Photos`, true)
	if err != nil || !ValidShareID(a.ID) {
		t.Fatalf("add: %v %+v", err, a)
	}
	if _, err := s.AddShare("photos", `E:\Other`, true); !errors.Is(err, ErrConflict) {
		t.Fatalf("case-insensitive duplicate name: %v", err)
	}
	name := "Pictures"
	rw := false
	up, err := s.UpdateShare(a.ID, ShareUpdate{Name: &name, ReadOnly: &rw})
	if err != nil || up.Name != "Pictures" || up.ReadOnly {
		t.Fatalf("update: %v %+v", err, up)
	}
	if _, err := s.GetShare("'; DROP TABLE shares;--"); !errors.Is(err, ErrNotFound) {
		t.Fatal("malformed id must be not-found")
	}
	if err := s.DeleteShare(a.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteShare(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("double delete")
	}
}

func TestLastAdminProtection(t *testing.T) {
	s, _ := open(t)
	adm, _ := s.AddUser("admin", "hash", "admin")
	if err := s.UpdateUser(adm.ID, "reader", false); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demote last admin: %v", err)
	}
	if err := s.DeleteUser(adm.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("delete last admin: %v", err)
	}
	s.AddUser("bob", "hash", "admin")
	if err := s.UpdateUser(adm.ID, "reader", false); err != nil {
		t.Fatalf("demote with a second admin present: %v", err)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s, _ := open(t)
	u, _ := s.AddUser("amy", "hash", "reader")
	tok, csrf, err := s.CreateSession(u.ID, time.Hour)
	if err != nil || len(tok) != 64 || csrf == "" {
		t.Fatalf("create: %v", err)
	}
	ss, got, err := s.LookupSession(tok)
	if err != nil || got.ID != u.ID || ss.CSRF != csrf {
		t.Fatalf("lookup: %v", err)
	}
	var n int
	s.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE token_hash = ?", tok).Scan(&n)
	if n != 0 {
		t.Fatal("raw session token stored in database")
	}
	if err := s.SetPassword(u.ID, "newhash"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.LookupSession(tok); err == nil {
		t.Fatal("password change must revoke sessions")
	}
	tok2, _, _ := s.CreateSession(u.ID, time.Hour)
	s.UpdateUser(u.ID, "reader", true)
	if _, _, err := s.LookupSession(tok2); err == nil {
		t.Fatal("disabled user keeps session")
	}
}

func TestAuditAndTransferTrimming(t *testing.T) {
	s, _ := open(t)
	for i := 0; i < maxTransfers+25; i++ {
		s.AddTransfer(Transfer{User: "u", Share: "s", Path: "p", Direction: "download", Bytes: int64(i), Status: "ok", IP: "1.2.3.4"})
	}
	tr, _ := s.RecentTransfers(10000)
	if len(tr) != maxTransfers {
		t.Fatalf("transfers kept: %d", len(tr))
	}
	if tr[0].Bytes != int64(maxTransfers+24) {
		t.Fatal("newest-first order broken")
	}
	s.AddAudit("a", "ip", "e", "d")
	if ev, _ := s.RecentAudit(5); len(ev) != 1 {
		t.Fatal("audit")
	}
}

func TestBackupIsConsistentCopy(t *testing.T) {
	s, _ := open(t)
	s.AddUser("admin", "hash", "admin")
	dest := filepath.Join(t.TempDir(), "backup.db")
	if err := s.BackupTo(dest); err != nil {
		t.Fatal(err)
	}
	if err := s.BackupTo(dest); err == nil {
		t.Fatal("backup must not overwrite")
	}
	b, _, err := Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if n, _ := b.CountUsers(); n != 1 {
		t.Fatalf("backup users = %d", n)
	}
}

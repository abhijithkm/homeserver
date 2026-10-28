package auth

import (
	"strings"
	"testing"
	"time"
)

func TestHashVerify(t *testing.T) {
	h, err := Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$") {
		t.Fatalf("unexpected format %q", h)
	}
	if !Verify("correct horse", h) || Verify("wrong", h) || Verify("correct horse", "garbage") {
		t.Fatal("verify mismatch")
	}
	h2, _ := Hash("correct horse")
	if h == h2 {
		t.Fatal("salt not random")
	}
}

func TestPolicies(t *testing.T) {
	if ValidatePassword("short") == nil || ValidatePassword(strings.Repeat("x", 300)) == nil || ValidatePassword("long enough") != nil {
		t.Fatal("password policy")
	}
	if ValidateUsername("") == nil || ValidateUsername("a b") == nil || ValidateUsername("ok_user-1.x") != nil {
		t.Fatal("username policy")
	}
}

func TestLimiterLockoutAndReset(t *testing.T) {
	l := NewLimiter(3, time.Minute, time.Hour)
	now := time.Now()
	l.now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow("k"); !ok {
			t.Fatalf("locked too early at %d", i)
		}
		l.Fail("k")
	}
	ok, wait := l.Allow("k")
	if ok || wait <= 0 {
		t.Fatal("expected lockout")
	}
	now = now.Add(wait + time.Second)
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("lock should expire")
	}
	l.Fail("k")
	l.Reset("k")
	if ok, _ := l.Allow("k"); !ok {
		t.Fatal("reset failed")
	}
}

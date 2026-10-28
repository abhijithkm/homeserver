// Package auth provides password hashing and login rate limiting.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters (OWASP minimum profile: 19 MiB, t=2, p=1). Kept modest
// so a login does not spike RAM on a small laptop.
const (
	argonMem     = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	saltLen      = 16

	// MinPasswordLen is the shortest accepted password.
	MinPasswordLen = 8
	// MaxPasswordLen bounds hashing work.
	MaxPasswordLen = 256
)

// ValidatePassword enforces the password policy.
func ValidatePassword(p string) error {
	if len(p) < MinPasswordLen {
		return fmt.Errorf("password must be at least %d characters", MinPasswordLen)
	}
	if len(p) > MaxPasswordLen {
		return fmt.Errorf("password must be at most %d characters", MaxPasswordLen)
	}
	return nil
}

// ValidateUsername enforces the username policy.
func ValidateUsername(u string) error {
	if len(u) < 1 || len(u) > 32 {
		return errors.New("username must be 1-32 characters")
	}
	for _, r := range u {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-' || r == '.') {
			return errors.New("username may contain letters, digits, '.', '_' and '-' only")
		}
	}
	return nil
}

// Hash returns an encoded argon2id hash.
func Hash(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMem, argonThreads, argonKeyLen)
	debug.FreeOSMemory()
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, argonMem, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify checks a password against an encoded hash in constant time.
func Verify(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil || m > 256*1024 || t > 10 || p == 0 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[4])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[5])
	if err1 != nil || err2 != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	debug.FreeOSMemory()
	return subtle.ConstantTimeCompare(got, want) == 1
}

// DummyVerify burns comparable CPU time for unknown usernames so response
// timing does not reveal which usernames exist.
func DummyVerify(password string) {
	_ = Verify(password, getDummyHash())
}

var (
	dummyOnce sync.Once
	dummyHash string
)

func getDummyHash() string {
	dummyOnce.Do(func() { dummyHash, _ = Hash("dummy-password-for-timing") })
	return dummyHash
}

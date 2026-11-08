package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"

	"golang.org/x/crypto/argon2"
)

// hashSem serialises argon2 runs so concurrent logins cannot multiply the
// 19 MiB working set, which also blunts login floods as a memory DoS.
var hashSem = make(chan struct{}, 1)

// deriveKey runs argon2id under hashSem and hands the freed memory back to the
// OS right away: Go's scavenger would otherwise keep ~20 MiB resident for
// minutes after every login, which matters for the panel's small-footprint goal.
func deriveKey(pw string, salt []byte, iters, mem uint32, threads uint8, keyLen uint32) []byte {
	hashSem <- struct{}{}
	defer func() {
		<-hashSem
		debug.FreeOSMemory()
	}()
	return argon2.IDKey([]byte(pw), salt, iters, mem, threads, keyLen)
}

// argon2id parameters, following the OWASP recommended minimum
// (19 MiB, 2 iterations, 1 lane). Chosen over the 64 MiB variant because the
// panel targets < 60 MB RSS on small boxes; see hashSem below.
const (
	argonTime    = 2
	argonMemory  = 19 * 1024
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16

	MinPasswordLen = 8
	MaxPasswordLen = 256
)

var ErrPasswordTooShort = errors.New("password too short")
var ErrPasswordTooLong = errors.New("password too long")

// ValidatePassword enforces length limits. Complexity rules are deliberately
// absent; length is what matters with argon2id.
func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	if len(pw) > MaxPasswordLen {
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns a PHC-formatted argon2id hash.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	key := deriveKey(pw, salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword checks pw against a hash produced by HashPassword.
// It returns false (not an error) for a wrong password, and an error only for
// a malformed hash.
func VerifyPassword(hash, pw string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var mem, iters uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iters, &threads); err != nil {
		return false, fmt.Errorf("parse argon2 params: %w", err)
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("decode salt: %w", err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("decode hash: %w", err)
	}
	got := deriveKey(pw, salt, iters, mem, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

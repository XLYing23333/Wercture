// Package auth hashes passwords and checks account names.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonMemoryKiB = 64 * 1024
	argonTime      = 3
	argonThreads   = 1
	argonKeyLen    = 32
	argonSaltLen   = 16

	// MinPasswordLen and MaxPasswordLen bound a password in bytes.
	MinPasswordLen = 6
	MaxPasswordLen = 1024
)

// ErrPasswordLength means the password is outside the accepted byte length.
var ErrPasswordLength = errors.New("password length out of range")

// ErrUsername means the account name is outside the accepted form.
var ErrUsername = errors.New("invalid username")

// ErrParams means a stored PHC string asks for a cost outside the allowed range.
var ErrParams = errors.New("argon2 parameters out of range")

// Hasher creates and checks PHC strings.
type Hasher interface {
	Hash(password []byte) (string, error)
	Verify(password []byte, phc string) (bool, error)
}

// Argon is an Argon2id hasher using the Weroxy cost parameters.
type Argon struct{}

// NewArgon returns the production hasher.
func NewArgon() *Argon { return &Argon{} }

// ValidUsername reports whether name is 1–64 ASCII letters, digits, dot, underscore, or hyphen.
func ValidUsername(name string) bool {
	if len(name) == 0 || len(name) > 64 {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// ValidatePassword checks the accepted byte length.
func ValidatePassword(password []byte) error {
	if len(password) < MinPasswordLen || len(password) > MaxPasswordLen {
		return ErrPasswordLength
	}
	return nil
}

// Hash returns a PHC string. The password is not retained.
func (a *Argon) Hash(password []byte) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("salt: %w", err)
	}
	sum := argon2.IDKey(password, salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return formatPHC(argonMemoryKiB, argonTime, argonThreads, salt, sum), nil
}

// Verify checks a password against a PHC string.
func (a *Argon) Verify(password []byte, phc string) (bool, error) {
	mem, timeCost, threads, salt, want, err := parsePHC(phc)
	if err != nil {
		return false, err
	}
	got := argon2.IDKey(password, salt, timeCost, mem, threads, argonKeyLen)
	if len(got) != len(want) {
		return false, nil
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

func formatPHC(memory, timeCost uint32, threads uint8, salt, sum []byte) string {
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		memory, timeCost, threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(sum))
}

func parsePHC(phc string) (memory, timeCost uint32, threads uint8, salt, sum []byte, err error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return 0, 0, 0, nil, nil, ErrParams
	}
	var mem, tim, par uint32
	if _, scanErr := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &tim, &par); scanErr != nil {
		return 0, 0, 0, nil, nil, ErrParams
	}
	if mem < 8*1024 || mem > 256*1024 || tim < 2 || tim > 5 || par < 1 || par > 4 {
		return 0, 0, 0, nil, nil, ErrParams
	}
	salt, err = base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return 0, 0, 0, nil, nil, ErrParams
	}
	sum, err = base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(sum) != argonKeyLen {
		return 0, 0, 0, nil, nil, ErrParams
	}
	return mem, tim, uint8(par), salt, sum, nil
}

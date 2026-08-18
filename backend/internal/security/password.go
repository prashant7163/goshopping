// Package security holds the cryptographic pieces the auth flow is built from:
// password hashing and the two token formats. It knows nothing about HTTP or
// the database, so the rules it enforces can be read in one place.
package security

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordLength is counted in characters, which is what a person typing
	// one perceives.
	MinPasswordLength = 8

	// MaxPasswordBytes is counted in bytes, because that is where bcrypt stops
	// reading: beyond 72 bytes the tail simply would not affect the hash.
	MaxPasswordBytes = 72
)

var (
	ErrPasswordTooShort = fmt.Errorf("password must be at least %d characters", MinPasswordLength)
	ErrPasswordTooLong  = fmt.Errorf("password must be at most %d bytes", MaxPasswordBytes)

	// ErrPasswordMismatch reports a failed comparison. It is deliberately the
	// only distinction Compare draws, so a caller cannot accidentally report
	// something more specific to a client.
	ErrPasswordMismatch = errors.New("password does not match")
)

// ValidatePassword applies the password policy. It is exported because signup,
// password change, and the frontend's mirrored rules must all agree on it.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(password) > MaxPasswordBytes {
		return ErrPasswordTooLong
	}
	return nil
}

// Hasher hashes and verifies passwords at a configured bcrypt cost.
type Hasher struct {
	cost int

	// decoy is a hash of a random password, computed at most once and only if a
	// login ever misses. See Decoy.
	decoy func() string
}

// NewHasher returns a Hasher at the given bcrypt cost, which config has already
// bounded to bcrypt's own 4..31.
func NewHasher(cost int) *Hasher {
	h := &Hasher{cost: cost}
	h.decoy = sync.OnceValue(func() string {
		password, err := RandomToken(32)
		if err != nil {
			// Only reachable if the system CSPRNG fails, in which case a fixed
			// string is still a fine input to a hash nobody ever matches
			// against.
			password = "decoy"
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
		if err != nil {
			return ""
		}
		return string(hash)
	})
	return h
}

// Hash applies the policy and returns the bcrypt hash of the password.
func (h *Hasher) Hash(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), h.cost)
	if err != nil {
		return "", fmt.Errorf("hashing password: %w", err)
	}
	return string(hash), nil
}

// Compare checks a password against a stored hash, returning ErrPasswordMismatch
// when they differ.
func (h *Hasher) Compare(hash, password string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, bcrypt.ErrMismatchedHashAndPassword),
		// A password past bcrypt's limit cannot have produced any hash this
		// application stored, so it is a mismatch rather than a failure.
		errors.Is(err, bcrypt.ErrPasswordTooLong):
		return ErrPasswordMismatch
	default:
		// A malformed stored hash lands here. That is a data problem worth
		// surfacing as a 500, not an authentication answer.
		return fmt.Errorf("comparing password: %w", err)
	}
}

// Decoy performs the same work a real comparison would, for use when no account
// matched. Without it, a login for an unregistered address returns in
// microseconds while a real one takes the full bcrypt cost — an enumeration
// oracle that the deliberately identical error message would not close.
func (h *Hasher) Decoy(password string) {
	_ = bcrypt.CompareHashAndPassword([]byte(h.decoy()), []byte(password))
}

// RandomToken returns n cryptographically random bytes in unpadded base64url,
// which is safe to place in a JSON body, a header, or a URL unescaped.
func RandomToken(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("reading random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

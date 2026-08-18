package security

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	// tokenIssuer is checked on parse, so a token minted by some other service
	// sharing the secret is still rejected.
	tokenIssuer = "goshopping"

	// jtiBytes sizes the access token's identifier. It only has to be unique
	// and unguessable, not to carry meaning (ADR-022).
	jtiBytes = 16

	// refreshTokenBytes is the length of the opaque refresh token, as specified
	// in ADR-008.
	refreshTokenBytes = 32
)

// ErrInvalidToken reports a token that failed to parse, was signed with another
// key or algorithm, or has expired.
var ErrInvalidToken = errors.New("invalid access token")

// Tokens mints and verifies the two token formats. Access tokens are signed
// JWTs; refresh tokens are opaque random strings stored only as a hash.
type Tokens struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewTokens(secret string, accessTTL, refreshTTL time.Duration) *Tokens {
	return &Tokens{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

// AccessTTL is the lifetime of issued access tokens, which handlers report to
// the client as expires_in.
func (t *Tokens) AccessTTL() time.Duration { return t.accessTTL }

// RefreshTTL is the lifetime of issued refresh tokens, and therefore of a
// session that is never refreshed.
func (t *Tokens) RefreshTTL() time.Duration { return t.refreshTTL }

// AccessToken is a freshly minted access token. JTI is stored on the session
// row; Value is what the client sends back.
type AccessToken struct {
	Value     string
	JTI       string
	ExpiresAt time.Time
}

// NewAccess signs an access token for the user, carrying a random jti that ties
// it to exactly one session row.
func (t *Tokens) NewAccess(userID string, now time.Time) (AccessToken, error) {
	jti, err := RandomToken(jtiBytes)
	if err != nil {
		return AccessToken{}, err
	}

	expiresAt := now.Add(t.accessTTL)
	claims := jwt.RegisteredClaims{
		Issuer:    tokenIssuer,
		Subject:   userID,
		ID:        jti,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.secret)
	if err != nil {
		return AccessToken{}, fmt.Errorf("signing access token: %w", err)
	}

	return AccessToken{Value: signed, JTI: jti, ExpiresAt: expiresAt}, nil
}

// RefreshToken is a freshly minted refresh token. Value goes to the client and
// is never stored; Hash is what the session row keeps (ADR-008).
type RefreshToken struct {
	Value     string
	Hash      []byte
	ExpiresAt time.Time
}

// NewRefresh generates an opaque refresh token and its hash.
func (t *Tokens) NewRefresh(now time.Time) (RefreshToken, error) {
	value, err := RandomToken(refreshTokenBytes)
	if err != nil {
		return RefreshToken{}, err
	}
	return RefreshToken{
		Value:     value,
		Hash:      HashRefreshToken(value),
		ExpiresAt: now.Add(t.refreshTTL),
	}, nil
}

// HashRefreshToken returns the raw SHA-256 digest stored in the session row.
// SHA-256 rather than bcrypt because the input is 32 random bytes: there is no
// guessable password here to slow an attacker down against, and this hash is
// computed on every refresh.
func HashRefreshToken(value string) []byte {
	sum := sha256.Sum256([]byte(value))
	return sum[:]
}

// AccessClaims is the part of a verified token the application uses.
type AccessClaims struct {
	UserID    string
	JTI       string
	ExpiresAt time.Time
}

// ParseAccess verifies the signature and the time-based claims, returning
// ErrInvalidToken for anything it will not accept. A valid signature only means
// the token was issued here — the session lookup decides whether it still
// counts (ADR-007).
func (t *Tokens) ParseAccess(raw string) (AccessClaims, error) {
	var claims jwt.RegisteredClaims

	_, err := jwt.ParseWithClaims(raw, &claims,
		func(*jwt.Token) (any, error) { return t.secret, nil },
		// Pinning the algorithm is what stops a token that claims alg "none",
		// or an RS256 token verified with the public key as an HMAC secret.
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return AccessClaims{}, fmt.Errorf("%w: %v", ErrInvalidToken, err)
	}

	if claims.Subject == "" || claims.ID == "" || claims.ExpiresAt == nil {
		return AccessClaims{}, fmt.Errorf("%w: missing subject, id, or expiry", ErrInvalidToken)
	}

	return AccessClaims{
		UserID:    claims.Subject,
		JTI:       claims.ID,
		ExpiresAt: claims.ExpiresAt.Time,
	}, nil
}

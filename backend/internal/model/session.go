package model

import "time"

// Session is one login. The row, not the token, decides whether a request is
// authenticated (ADR-007): the signature proves the token was issued here, and
// the session decides whether it still counts.
//
// Session is never serialized — it holds token material — so it carries no JSON
// tags. The active-sessions view planned for Step 2 will render a summary type.
type Session struct {
	ID     string
	UserID string

	// AccessJTI is the jti claim of the access token currently paired with this
	// session. Rotation replaces it, which is what retires the previous access
	// token before its own expiry.
	AccessJTI string

	// RefreshHash is the SHA-256 of the current refresh token, and
	// PreviousRefreshHash the one generation of history reuse detection needs
	// (ADR-009). The tokens themselves are never stored.
	RefreshHash         []byte
	PreviousRefreshHash []byte

	UserAgent string
	ExpiresAt time.Time
	RevokedAt *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// IsActive reports whether the session still authenticates at the given moment.
func (s *Session) IsActive(now time.Time) bool {
	return s.RevokedAt == nil && now.Before(s.ExpiresAt)
}

package repository

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/prashant7163/goshopping/backend/internal/model"
)

const sessionColumns = `id, user_id, access_jti, refresh_hash, previous_refresh_hash,
	user_agent, expires_at, revoked_at, created_at, updated_at`

// sessionColumnsQualified is sessionColumns with an "s." prefix, for the join
// against users. Kept next to sessionColumns so the two lists stay in step.
const sessionColumnsQualified = `s.id, s.user_id, s.access_jti, s.refresh_hash, s.previous_refresh_hash,
	s.user_agent, s.expires_at, s.revoked_at, s.created_at, s.updated_at`

// SessionRepository reads and writes the sessions table.
type SessionRepository struct {
	db DBTX
}

// CreateSessionParams is the set of values a new login row needs. The rest are
// database defaults.
type CreateSessionParams struct {
	UserID      string
	AccessJTI   string
	RefreshHash []byte
	UserAgent   string
	ExpiresAt   time.Time
}

// Create inserts one login session.
func (r *SessionRepository) Create(ctx context.Context, p CreateSessionParams) (*model.Session, error) {
	const query = `
		INSERT INTO sessions (user_id, access_jti, refresh_hash, user_agent, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING ` + sessionColumns

	session, err := scanSession(r.db.QueryRow(ctx, query,
		p.UserID, p.AccessJTI, p.RefreshHash, p.UserAgent, p.ExpiresAt))
	if err != nil {
		return nil, wrapQueryErr("creating session", err)
	}
	return session, nil
}

// ByAccessJTIWithUser returns the session carrying the given jti together with
// its owner, in one query. This is the lookup every authenticated request makes
// (ADR-007), and joining here means /users/me costs no second round trip.
func (r *SessionRepository) ByAccessJTIWithUser(ctx context.Context, jti string) (*model.Session, *model.User, error) {
	const query = `
		SELECT ` + sessionColumnsQualified + `, ` + userColumnsQualified + `
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.access_jti = $1`

	var s model.Session
	var u model.User
	err := r.db.QueryRow(ctx, query, jti).Scan(
		&s.ID, &s.UserID, &s.AccessJTI, &s.RefreshHash, &s.PreviousRefreshHash,
		&s.UserAgent, &s.ExpiresAt, &s.RevokedAt, &s.CreatedAt, &s.UpdatedAt,
		&u.ID, &u.Email, &u.FullName, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		return nil, nil, wrapQueryErr("finding session by access jti", err)
	}
	return &s, &u, nil
}

// ByRefreshHash returns the session whose current refresh token hashes to hash.
func (r *SessionRepository) ByRefreshHash(ctx context.Context, hash []byte) (*model.Session, error) {
	const query = `SELECT ` + sessionColumns + ` FROM sessions WHERE refresh_hash = $1`

	session, err := scanSession(r.db.QueryRow(ctx, query, hash))
	if err != nil {
		return nil, wrapQueryErr("finding session by refresh hash", err)
	}
	return session, nil
}

// ByPreviousRefreshHash returns the session that has already rotated past the
// given hash. A match means a token the legitimate client replaced is being
// presented again, which is the replay ADR-009 exists to catch.
func (r *SessionRepository) ByPreviousRefreshHash(ctx context.Context, hash []byte) (*model.Session, error) {
	const query = `SELECT ` + sessionColumns + ` FROM sessions WHERE previous_refresh_hash = $1`

	session, err := scanSession(r.db.QueryRow(ctx, query, hash))
	if err != nil {
		return nil, wrapQueryErr("finding session by previous refresh hash", err)
	}
	return session, nil
}

// RotateSessionParams describes one rotation. ExpectedRefreshHash is the hash
// the caller read a moment ago; the update only applies if it is still current.
type RotateSessionParams struct {
	ID                  string
	ExpectedRefreshHash []byte
	AccessJTI           string
	RefreshHash         []byte
	ExpiresAt           time.Time
}

// Rotate replaces the session's token pair in place, keeping sessions.id stable
// for the life of the login (ADR-008), and files the outgoing refresh hash as
// the previous generation.
//
// The WHERE clause makes this a compare-and-swap: two requests presenting the
// same refresh token race, exactly one updates a row, and the loser gets
// ErrNotFound rather than quietly rotating a second time.
func (r *SessionRepository) Rotate(ctx context.Context, p RotateSessionParams) (*model.Session, error) {
	const query = `
		UPDATE sessions
		SET access_jti            = $3,
		    refresh_hash          = $4,
		    previous_refresh_hash = $2,
		    expires_at            = $5
		WHERE id           = $1
		  AND refresh_hash = $2
		  AND revoked_at IS NULL
		  AND expires_at  > now()
		RETURNING ` + sessionColumns

	session, err := scanSession(r.db.QueryRow(ctx, query,
		p.ID, p.ExpectedRefreshHash, p.AccessJTI, p.RefreshHash, p.ExpiresAt))
	if err != nil {
		return nil, wrapQueryErr("rotating session", err)
	}
	return session, nil
}

// Revoke ends one session. An already-revoked session is left as it is, so the
// original revocation time survives.
func (r *SessionRepository) Revoke(ctx context.Context, id string) error {
	const query = `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`

	tag, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return wrapQueryErr("revoking session", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// RevokeAllForUser ends every live session a user has and reports how many were
// affected. It backs both the password change (ADR-010) and the reuse response
// (ADR-009).
func (r *SessionRepository) RevokeAllForUser(ctx context.Context, userID string) (int64, error) {
	const query = `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`

	tag, err := r.db.Exec(ctx, query, userID)
	if err != nil {
		return 0, wrapQueryErr("revoking sessions for user", err)
	}
	return tag.RowsAffected(), nil
}

// DeleteReapable removes sessions that stopped mattering before cutoff: expired
// ones, and revoked ones whose own expiry may still be weeks away. Both are
// dead weight; keeping them only grows the table the middleware searches.
func (r *SessionRepository) DeleteReapable(ctx context.Context, cutoff time.Time) (int64, error) {
	const query = `
		DELETE FROM sessions
		WHERE expires_at < $1
		   OR (revoked_at IS NOT NULL AND revoked_at < $1)`

	tag, err := r.db.Exec(ctx, query, cutoff)
	if err != nil {
		return 0, wrapQueryErr("deleting expired sessions", err)
	}
	return tag.RowsAffected(), nil
}

func scanSession(row pgx.Row) (*model.Session, error) {
	var s model.Session
	err := row.Scan(
		&s.ID, &s.UserID, &s.AccessJTI, &s.RefreshHash, &s.PreviousRefreshHash,
		&s.UserAgent, &s.ExpiresAt, &s.RevokedAt, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

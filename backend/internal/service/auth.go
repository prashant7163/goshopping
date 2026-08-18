// Package service holds the application's rules. Handlers translate HTTP into
// these calls and back; nothing here knows about echo, headers, or status
// codes, and errors leave as the sentinels below for the handler to render.
package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/prashant7163/goshopping/backend/internal/model"
	"github.com/prashant7163/goshopping/backend/internal/repository"
	"github.com/prashant7163/goshopping/backend/internal/security"
)

var (
	// ErrEmailTaken reports that the address is already registered. Only signup
	// returns it; login must never reveal the same fact.
	ErrEmailTaken = errors.New("email already registered")

	// ErrInvalidCredentials covers both an unknown email and a wrong password,
	// so the response cannot be used to test whether an account exists.
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrInvalidToken covers a malformed, expired, revoked, or replayed token.
	// The client's response is the same in every case: authenticate again.
	ErrInvalidToken = errors.New("invalid or expired token")

	// ErrUserNotFound reports a token or session that outlived its user.
	ErrUserNotFound = errors.New("user no longer exists")

	// ErrPasswordUnchanged rejects a password change that changes nothing,
	// which would sign out every other device for no benefit.
	ErrPasswordUnchanged = errors.New("new password must differ from the current password")
)

// maxUserAgentLength bounds what is stored from the User-Agent header. The
// column is unbounded text; the value is only ever shown back to the user in
// the Step 2 sessions view, so a truncated string is no loss.
const maxUserAgentLength = 512

// Auth implements every authentication flow. It is the only writer of the
// sessions table.
type Auth struct {
	store  *repository.Store
	hasher *security.Hasher
	tokens *security.Tokens
	logger *slog.Logger

	// now is a field so time-dependent behaviour can be driven in tests.
	now func() time.Time
}

func NewAuth(store *repository.Store, hasher *security.Hasher, tokens *security.Tokens, logger *slog.Logger) *Auth {
	return &Auth{store: store, hasher: hasher, tokens: tokens, logger: logger, now: time.Now}
}

// Result is a freshly issued token pair with the account it belongs to. Signup,
// login, refresh, and password change all answer with one.
type Result struct {
	AccessToken  string
	RefreshToken string
	// ExpiresIn is the access token's remaining life in seconds, the unit the
	// OAuth-shaped response the frontend expects uses.
	ExpiresIn int64
	User      *model.User
}

type SignupInput struct {
	Email     string
	FullName  string
	Password  string
	UserAgent string
}

// Signup registers an account and signs it in, so the client does not have to
// immediately post the same credentials to /login (ADR-024).
func (a *Auth) Signup(ctx context.Context, in SignupInput) (*Result, error) {
	hash, err := a.hasher.Hash(in.Password)
	if err != nil {
		return nil, err
	}

	user, err := a.store.Users.Create(ctx, normalizeEmail(in.Email), strings.TrimSpace(in.FullName), hash)
	if err != nil {
		if errors.Is(err, repository.ErrEmailTaken) {
			return nil, ErrEmailTaken
		}
		return nil, err
	}

	a.logger.InfoContext(ctx, "user registered", slog.String("user_id", user.ID))
	return a.issue(ctx, a.store, user, in.UserAgent)
}

type LoginInput struct {
	Email     string
	Password  string
	UserAgent string
}

// Login verifies credentials and opens a session.
func (a *Auth) Login(ctx context.Context, in LoginInput) (*Result, error) {
	user, err := a.store.Users.ByEmail(ctx, normalizeEmail(in.Email))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// Spend the same time a real comparison would before answering, so
			// the response tells an attacker nothing the error message does not.
			a.hasher.Decoy(in.Password)
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	if err := a.hasher.Compare(user.PasswordHash, in.Password); err != nil {
		if errors.Is(err, security.ErrPasswordMismatch) {
			return nil, ErrInvalidCredentials
		}
		return nil, err
	}

	return a.issue(ctx, a.store, user, in.UserAgent)
}

// Refresh rotates a session's token pair in place and returns the new one.
//
// A token that matches no live session is not simply rejected: it may be one
// this session has already rotated past, which is a replay and revokes
// everything the user has (ADR-009).
func (a *Auth) Refresh(ctx context.Context, refreshToken string) (*Result, error) {
	if strings.TrimSpace(refreshToken) == "" {
		return nil, ErrInvalidToken
	}

	presented := security.HashRefreshToken(refreshToken)
	now := a.now()

	session, err := a.store.Sessions.ByRefreshHash(ctx, presented)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, a.handleUnknownRefresh(ctx, presented)
		}
		return nil, err
	}

	if !session.IsActive(now) {
		return nil, ErrInvalidToken
	}

	user, err := a.store.Users.ByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, err
	}

	access, refresh, err := a.mint(user.ID, now)
	if err != nil {
		return nil, err
	}

	_, err = a.store.Sessions.Rotate(ctx, repository.RotateSessionParams{
		ID:                  session.ID,
		ExpectedRefreshHash: presented,
		AccessJTI:           access.JTI,
		RefreshHash:         refresh.Hash,
		ExpiresAt:           refresh.ExpiresAt,
	})
	if err != nil {
		// No row matched, so the session was revoked or rotated between the
		// read above and this write. Either way the presented token is spent.
		if errors.Is(err, repository.ErrNotFound) {
			return nil, ErrInvalidToken
		}
		return nil, err
	}

	return a.result(access, refresh, user), nil
}

// handleUnknownRefresh decides what an unrecognized refresh token means. If it
// is the generation a session has already rotated past, the legitimate client's
// token is presumed stolen and every session for that user ends.
func (a *Auth) handleUnknownRefresh(ctx context.Context, presented []byte) error {
	session, err := a.store.Sessions.ByPreviousRefreshHash(ctx, presented)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return ErrInvalidToken
		}
		return err
	}

	revoked, err := a.store.Sessions.RevokeAllForUser(ctx, session.UserID)
	if err != nil {
		return err
	}

	a.logger.WarnContext(ctx, "refresh token reuse detected, all sessions revoked",
		slog.String("user_id", session.UserID),
		slog.String("session_id", session.ID),
		slog.Int64("sessions_revoked", revoked),
	)
	return ErrInvalidToken
}

// Logout ends one session. Calling it twice is not an error: the caller's goal,
// a session that no longer authenticates, is already met.
func (a *Auth) Logout(ctx context.Context, sessionID string) error {
	if err := a.store.Sessions.Revoke(ctx, sessionID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil
		}
		return err
	}
	return nil
}

// Authenticate resolves a bearer token to its session and user, and is what the
// auth middleware calls on every protected request. The signature is checked
// first so a forged token never reaches the database.
func (a *Auth) Authenticate(ctx context.Context, accessToken string) (*model.Session, *model.User, error) {
	claims, err := a.tokens.ParseAccess(accessToken)
	if err != nil {
		return nil, nil, ErrInvalidToken
	}

	session, user, err := a.store.Sessions.ByAccessJTIWithUser(ctx, claims.JTI)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			// The jti is unknown because the session was rotated past this
			// token, or reaped after expiry.
			return nil, nil, ErrInvalidToken
		}
		return nil, nil, err
	}

	if !session.IsActive(a.now()) {
		return nil, nil, ErrInvalidToken
	}

	// The jti is unique, so this can only disagree if a token were minted for
	// one user and filed against another's session. Checking costs nothing.
	if session.UserID != claims.UserID {
		a.logger.ErrorContext(ctx, "access token subject does not match its session",
			slog.String("session_id", session.ID),
			slog.String("session_user_id", session.UserID),
			slog.String("token_user_id", claims.UserID),
		)
		return nil, nil, ErrInvalidToken
	}

	return session, user, nil
}

type ChangePasswordInput struct {
	UserID          string
	CurrentPassword string
	NewPassword     string
	UserAgent       string
}

// ChangePassword verifies the current password, replaces it, ends every session
// the user has, and opens one new session — all in a single transaction, so the
// caller is never left signed out of an account whose password did change
// (ADR-010).
func (a *Auth) ChangePassword(ctx context.Context, in ChangePasswordInput) (*Result, error) {
	var result *Result

	err := a.store.Tx(ctx, func(tx *repository.Store) error {
		// Locked for the transaction: two concurrent changes must not both
		// verify against the hash only one of them will replace.
		user, err := tx.Users.ByIDForUpdate(ctx, in.UserID)
		if err != nil {
			if errors.Is(err, repository.ErrNotFound) {
				return ErrUserNotFound
			}
			return err
		}

		if err := a.hasher.Compare(user.PasswordHash, in.CurrentPassword); err != nil {
			if errors.Is(err, security.ErrPasswordMismatch) {
				return ErrInvalidCredentials
			}
			return err
		}

		if err := security.ValidatePassword(in.NewPassword); err != nil {
			return err
		}
		if a.hasher.Compare(user.PasswordHash, in.NewPassword) == nil {
			return ErrPasswordUnchanged
		}

		hash, err := a.hasher.Hash(in.NewPassword)
		if err != nil {
			return err
		}
		if err := tx.Users.UpdatePassword(ctx, user.ID, hash); err != nil {
			return err
		}

		revoked, err := tx.Sessions.RevokeAllForUser(ctx, user.ID)
		if err != nil {
			return err
		}

		user.PasswordHash = hash
		result, err = a.issue(ctx, tx, user, in.UserAgent)
		if err != nil {
			return err
		}

		a.logger.InfoContext(ctx, "password changed, sessions revoked",
			slog.String("user_id", user.ID),
			slog.Int64("sessions_revoked", revoked),
		)
		return nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

// issue mints a token pair and records the session it belongs to. store is a
// parameter rather than a.store so a caller inside a transaction gets the
// session row written in that same transaction.
func (a *Auth) issue(ctx context.Context, store *repository.Store, user *model.User, userAgent string) (*Result, error) {
	now := a.now()

	access, refresh, err := a.mint(user.ID, now)
	if err != nil {
		return nil, err
	}

	// The session outlives the access token: the refresh token's expiry is what
	// ends a login that is never renewed.
	if _, err := store.Sessions.Create(ctx, repository.CreateSessionParams{
		UserID:      user.ID,
		AccessJTI:   access.JTI,
		RefreshHash: refresh.Hash,
		UserAgent:   truncate(userAgent, maxUserAgentLength),
		ExpiresAt:   refresh.ExpiresAt,
	}); err != nil {
		return nil, err
	}

	return a.result(access, refresh, user), nil
}

func (a *Auth) mint(userID string, now time.Time) (security.AccessToken, security.RefreshToken, error) {
	access, err := a.tokens.NewAccess(userID, now)
	if err != nil {
		return security.AccessToken{}, security.RefreshToken{}, err
	}
	refresh, err := a.tokens.NewRefresh(now)
	if err != nil {
		return security.AccessToken{}, security.RefreshToken{}, err
	}
	return access, refresh, nil
}

func (a *Auth) result(access security.AccessToken, refresh security.RefreshToken, user *model.User) *Result {
	return &Result{
		AccessToken:  access.Value,
		RefreshToken: refresh.Value,
		ExpiresIn:    int64(a.tokens.AccessTTL().Seconds()),
		User:         user,
	}
}

// normalizeEmail trims surrounding whitespace. Case is left alone on purpose:
// the citext column compares case-insensitively (ADR-006), so the address stays
// as the user typed it while still being unique.
func normalizeEmail(email string) string { return strings.TrimSpace(email) }

// truncate cuts s to at most max bytes on a rune boundary, and drops anything
// that is not valid UTF-8. A text column rejects an invalid byte sequence, and a
// header is arbitrary bytes from a client.
func truncate(s string, max int) string {
	if len(s) > max {
		for max > 0 && !utf8.RuneStart(s[max]) {
			max--
		}
		s = s[:max]
	}
	return strings.ToValidUTF8(s, "")
}

package middleware

import (
	"context"
	"errors"
	"strings"

	"github.com/labstack/echo/v4"

	"github.com/prashant7163/goshopping/backend/internal/apierror"
	"github.com/prashant7163/goshopping/backend/internal/model"
	"github.com/prashant7163/goshopping/backend/internal/service"
)

// Keys under which RequireAuth stores what it resolved. They are read through
// the accessors below rather than directly, so the strings stay private to this
// package in practice.
const (
	userContextKey    = "auth.user"
	sessionContextKey = "auth.session"
)

const bearerPrefix = "Bearer "

// Authenticator resolves a bearer token to the session and user it belongs to.
// *service.Auth implements it; the interface is here so the middleware can be
// tested without a database.
type Authenticator interface {
	Authenticate(ctx context.Context, accessToken string) (*model.Session, *model.User, error)
}

// RequireAuth rejects any request without a valid access token, and puts the
// session and user it resolved into the context for the handler.
//
// "Valid" means more than a good signature: the session row is consulted on
// every request, which is what makes revocation immediate (ADR-007).
func RequireAuth(auth Authenticator) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			token, ok := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
			if !ok {
				return apierror.Unauthorized("missing or malformed Authorization header")
			}

			session, user, err := auth.Authenticate(c.Request().Context(), token)
			if err != nil {
				if errors.Is(err, service.ErrInvalidToken) {
					return apierror.Unauthorized("invalid or expired access token")
				}
				// A database failure is ours, not the caller's; answering 401
				// would tell them to sign in again over a transient outage.
				return apierror.Internal(err)
			}

			c.Set(sessionContextKey, session)
			c.Set(userContextKey, user)
			return next(c)
		}
	}
}

// bearerToken extracts the credential from an Authorization header. The scheme
// is matched case-insensitively, as RFC 7235 requires.
func bearerToken(header string) (string, bool) {
	if len(header) < len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
		return "", false
	}
	token := strings.TrimSpace(header[len(bearerPrefix):])
	if token == "" {
		return "", false
	}
	return token, true
}

// UserFrom returns the authenticated user. It reports false on a route that did
// not run RequireAuth, which is a wiring mistake rather than a client error.
func UserFrom(c echo.Context) (*model.User, bool) {
	user, ok := c.Get(userContextKey).(*model.User)
	return user, ok
}

// SessionFrom returns the session the request authenticated with.
func SessionFrom(c echo.Context) (*model.Session, bool) {
	session, ok := c.Get(sessionContextKey).(*model.Session)
	return session, ok
}

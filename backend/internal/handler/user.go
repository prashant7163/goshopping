package handler

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/prashant7163/goshopping/backend/internal/apierror"
	appmw "github.com/prashant7163/goshopping/backend/internal/middleware"
	"github.com/prashant7163/goshopping/backend/internal/service"
)

// User serves the /api/v1/users routes. Every one of them is behind the auth
// middleware.
type User struct {
	auth *service.Auth
}

func NewUser(auth *service.Auth) *User {
	return &User{auth: auth}
}

// Me returns the signed-in user's profile.
//
//	GET /api/v1/users/me -> 200.
//
// No query is needed: the middleware already loaded the user alongside the
// session it had to check anyway.
func (h *User) Me(c echo.Context) error {
	user, ok := appmw.UserFrom(c)
	if !ok {
		return apierror.Internal(errors.New("users/me route is missing the auth middleware"))
	}
	return c.JSON(http.StatusOK, user)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

// ChangePassword replaces the password and re-issues tokens.
//
//	PATCH /api/v1/users/me/password -> 200 with a fresh token pair.
//
// Every other device is signed out; the caller stays signed in by swapping in
// the pair returned here (ADR-010).
func (h *User) ChangePassword(c echo.Context) error {
	user, ok := appmw.UserFrom(c)
	if !ok {
		return apierror.Internal(errors.New("password route is missing the auth middleware"))
	}

	var req changePasswordRequest
	if err := c.Bind(&req); err != nil {
		return apierror.BadRequest("request body is not valid JSON")
	}

	f := fieldErrors{}
	validateRequired(f, "current_password", req.CurrentPassword, "current password is required")
	validatePassword(f, "new_password", req.NewPassword)
	if err := f.err(); err != nil {
		return err
	}

	result, err := h.auth.ChangePassword(c.Request().Context(), service.ChangePasswordInput{
		UserID:          user.ID,
		CurrentPassword: req.CurrentPassword,
		NewPassword:     req.NewPassword,
		UserAgent:       c.Request().UserAgent(),
	})
	if err != nil {
		// Here the caller is already authenticated, so naming which password is
		// wrong leaks nothing and saves them guessing.
		if errors.Is(err, service.ErrInvalidCredentials) {
			return apierror.Unauthorized("current password is incorrect")
		}
		return translate(err)
	}

	return c.JSON(http.StatusOK, newAuthResponse(result))
}

package handler

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/prashant7163/goshopping/backend/internal/apierror"
	appmw "github.com/prashant7163/goshopping/backend/internal/middleware"
	"github.com/prashant7163/goshopping/backend/internal/model"
	"github.com/prashant7163/goshopping/backend/internal/security"
	"github.com/prashant7163/goshopping/backend/internal/service"
)

// Auth serves the /api/v1/auth routes.
type Auth struct {
	auth *service.Auth
}

func NewAuth(auth *service.Auth) *Auth {
	return &Auth{auth: auth}
}

// authResponse is what every endpoint that issues tokens returns. User is the
// domain type, whose password hash is unexported to JSON at the source.
type authResponse struct {
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	TokenType    string      `json:"token_type"`
	ExpiresIn    int64       `json:"expires_in"`
	User         *model.User `json:"user"`
}

func newAuthResponse(r *service.Result) authResponse {
	return authResponse{
		AccessToken:  r.AccessToken,
		RefreshToken: r.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    r.ExpiresIn,
		User:         r.User,
	}
}

type signupRequest struct {
	Email    string `json:"email"`
	FullName string `json:"full_name"`
	Password string `json:"password"`
}

// Signup registers an account and signs it in.
//
//	POST /api/v1/auth/signup -> 201 with a token pair, 409 if the email is taken.
func (h *Auth) Signup(c echo.Context) error {
	var req signupRequest
	if err := c.Bind(&req); err != nil {
		return apierror.BadRequest("request body is not valid JSON")
	}

	f := fieldErrors{}
	validateEmail(f, req.Email)
	validateFullName(f, req.FullName)
	validatePassword(f, "password", req.Password)
	if err := f.err(); err != nil {
		return err
	}

	result, err := h.auth.Signup(c.Request().Context(), service.SignupInput{
		Email:     req.Email,
		FullName:  req.FullName,
		Password:  req.Password,
		UserAgent: c.Request().UserAgent(),
	})
	if err != nil {
		return translate(err)
	}

	return c.JSON(http.StatusCreated, newAuthResponse(result))
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// Login exchanges credentials for a token pair.
//
//	POST /api/v1/auth/login -> 200, or 401 for either a wrong password or an
//	address that was never registered — the two are indistinguishable on purpose.
func (h *Auth) Login(c echo.Context) error {
	var req loginRequest
	if err := c.Bind(&req); err != nil {
		return apierror.BadRequest("request body is not valid JSON")
	}

	// Only presence is checked. Applying the password policy here would reject
	// a wrong password with a different status than a wrong-but-plausible one,
	// which is the enumeration hint the identical message avoids.
	f := fieldErrors{}
	validateRequired(f, "email", req.Email, "email is required")
	validateRequired(f, "password", req.Password, "password is required")
	if err := f.err(); err != nil {
		return err
	}

	result, err := h.auth.Login(c.Request().Context(), service.LoginInput{
		Email:     req.Email,
		Password:  req.Password,
		UserAgent: c.Request().UserAgent(),
	})
	if err != nil {
		return translate(err)
	}

	return c.JSON(http.StatusOK, newAuthResponse(result))
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh rotates the token pair.
//
//	POST /api/v1/auth/refresh -> 200 with a new pair, 401 if the token is spent.
//
// Unauthenticated by design: the refresh token is the credential, and the
// access token it renews has usually expired by the time this is called.
func (h *Auth) Refresh(c echo.Context) error {
	var req refreshRequest
	if err := c.Bind(&req); err != nil {
		return apierror.BadRequest("request body is not valid JSON")
	}

	f := fieldErrors{}
	validateRequired(f, "refresh_token", req.RefreshToken, "refresh token is required")
	if err := f.err(); err != nil {
		return err
	}

	result, err := h.auth.Refresh(c.Request().Context(), req.RefreshToken)
	if err != nil {
		return translate(err)
	}

	return c.JSON(http.StatusOK, newAuthResponse(result))
}

// Logout revokes the session the request authenticated with, leaving the user's
// other devices signed in.
//
//	POST /api/v1/auth/logout -> 204.
func (h *Auth) Logout(c echo.Context) error {
	session, ok := appmw.SessionFrom(c)
	if !ok {
		return apierror.Internal(errors.New("logout route is missing the auth middleware"))
	}

	if err := h.auth.Logout(c.Request().Context(), session.ID); err != nil {
		return translate(err)
	}

	return c.NoContent(http.StatusNoContent)
}

// translate maps a service error to the response the client gets. It is the
// single place where a domain failure becomes a status code, which is what
// keeps the service layer free of HTTP.
func translate(err error) error {
	switch {
	case errors.Is(err, service.ErrEmailTaken):
		return apierror.Conflict("an account with that email already exists")
	case errors.Is(err, service.ErrInvalidCredentials):
		return apierror.Unauthorized("email or password is incorrect")
	case errors.Is(err, service.ErrInvalidToken):
		return apierror.Unauthorized("invalid or expired token")
	case errors.Is(err, service.ErrUserNotFound):
		return apierror.Unauthorized("this account no longer exists")
	case errors.Is(err, service.ErrPasswordUnchanged):
		return apierror.BadRequest(err.Error())
	case errors.Is(err, security.ErrPasswordTooShort), errors.Is(err, security.ErrPasswordTooLong):
		// Reachable only if a policy rule is enforced in the service but not
		// mirrored by the request validation above.
		return apierror.ValidationFailed(err.Error())
	default:
		return apierror.Internal(err)
	}
}

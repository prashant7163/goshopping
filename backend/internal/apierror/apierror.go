// Package apierror defines the single error shape every endpoint returns and
// the Echo handler that produces it. Keeping one envelope here means the
// frontend has exactly one error format to parse.
//
// Wire format:
//
//	{"error": {"code": "not_found", "message": "resource not found"}}
package apierror

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
)

// Machine-readable codes. The frontend switches on these; `message` is for
// humans and may be reworded freely.
const (
	CodeBadRequest       = "bad_request"
	CodeValidationFailed = "validation_failed"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeConflict         = "conflict"
	CodeTooManyRequests  = "too_many_requests"
	CodeInternal         = "internal_error"
	CodeUnavailable      = "service_unavailable"
)

// Body is the payload nested under the "error" key.
type Body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

type envelope struct {
	Error Body `json:"error"`
}

// Error is an application error carrying the HTTP status and public message to
// send. An optional wrapped cause is logged but never serialized.
type Error struct {
	Status  int
	Code    string
	Message string
	Details any

	cause error
}

func (e *Error) Error() string { return e.Message }

func (e *Error) Unwrap() error { return e.cause }

// WithCause attaches an internal error for logging. The cause never reaches the
// client, so it is safe to wrap driver or database errors here.
func (e *Error) WithCause(err error) *Error {
	e.cause = err
	return e
}

// WithDetails attaches structured context, such as per-field validation errors.
func (e *Error) WithDetails(details any) *Error {
	e.Details = details
	return e
}

func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func BadRequest(message string) *Error {
	return New(http.StatusBadRequest, CodeBadRequest, message)
}

func ValidationFailed(message string) *Error {
	return New(http.StatusBadRequest, CodeValidationFailed, message)
}

func Unauthorized(message string) *Error {
	return New(http.StatusUnauthorized, CodeUnauthorized, message)
}

func Forbidden(message string) *Error {
	return New(http.StatusForbidden, CodeForbidden, message)
}

func NotFound(message string) *Error {
	return New(http.StatusNotFound, CodeNotFound, message)
}

func Conflict(message string) *Error {
	return New(http.StatusConflict, CodeConflict, message)
}

func Internal(cause error) *Error {
	return New(http.StatusInternalServerError, CodeInternal, "an unexpected error occurred").WithCause(cause)
}

// Handler returns Echo's central error handler. Every error leaving a route
// passes through here, so this is the only place that decides what a client
// gets to see.
func Handler(logger *slog.Logger) echo.HTTPErrorHandler {
	return func(err error, c echo.Context) {
		// A handler that already began writing cannot be given a new body.
		if c.Response().Committed {
			return
		}

		apiErr := translate(err)

		// 5xx means we got something wrong, so record the full cause. 4xx is the
		// caller's mistake and is already visible in the request log.
		if apiErr.Status >= http.StatusInternalServerError {
			logger.ErrorContext(c.Request().Context(), "request failed",
				slog.String("method", c.Request().Method),
				slog.String("path", c.Request().URL.Path),
				slog.String("error", err.Error()),
			)
		}

		var writeErr error
		if c.Request().Method == http.MethodHead {
			writeErr = c.NoContent(apiErr.Status)
		} else {
			writeErr = c.JSON(apiErr.Status, envelope{Error: Body{
				Code:    apiErr.Code,
				Message: apiErr.Message,
				Details: apiErr.Details,
			}})
		}
		if writeErr != nil {
			logger.ErrorContext(c.Request().Context(), "writing error response", slog.String("error", writeErr.Error()))
		}
	}
}

// translate reduces any error to something safe to serialize.
func translate(err error) *Error {
	var apiErr *Error
	if errors.As(err, &apiErr) {
		return apiErr
	}

	// Echo raises these for routing and binding failures before handlers run.
	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		message := http.StatusText(httpErr.Code)
		if m, ok := httpErr.Message.(string); ok && m != "" {
			message = m
		}
		return New(httpErr.Code, codeForStatus(httpErr.Code), message)
	}

	// Anything unrecognized is a bug. Report it generically; the real error is
	// logged by the caller.
	return Internal(err)
}

func codeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return CodeBadRequest
	case http.StatusUnauthorized:
		return CodeUnauthorized
	case http.StatusForbidden:
		return CodeForbidden
	case http.StatusNotFound:
		return CodeNotFound
	case http.StatusMethodNotAllowed:
		return CodeMethodNotAllowed
	case http.StatusConflict:
		return CodeConflict
	case http.StatusTooManyRequests:
		return CodeTooManyRequests
	case http.StatusServiceUnavailable:
		return CodeUnavailable
	default:
		if status >= http.StatusInternalServerError {
			return CodeInternal
		}
		return CodeBadRequest
	}
}

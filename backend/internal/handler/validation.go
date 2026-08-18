package handler

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/prashant7163/goshopping/backend/internal/apierror"
	"github.com/prashant7163/goshopping/backend/internal/security"
)

const (
	// maxEmailLength is the longest address SMTP permits.
	maxEmailLength = 254
	// maxFullNameLength is a sanity bound, not a rule about names.
	maxFullNameLength = 200
)

// fieldErrors collects per-field messages so one response can report every
// problem with a form, rather than the first one and then the next on retry.
type fieldErrors map[string]string

func (f fieldErrors) add(field, message string) {
	// First message per field wins: it is the most specific, since checks run
	// from most to least specific.
	if _, exists := f[field]; !exists {
		f[field] = message
	}
}

// err returns the API error to send, or nil when nothing failed. The map is
// carried in the envelope's details, which is what the frontend attaches to
// individual inputs.
func (f fieldErrors) err() error {
	if len(f) == 0 {
		return nil
	}
	return apierror.ValidationFailed("one or more fields are invalid").WithDetails(f)
}

// validateEmail requires a bare, parseable address. A display-name form such as
// "Ada <ada@example.com>" parses but is not what a signup form means, so the
// parsed address must be exactly what was submitted.
func validateEmail(f fieldErrors, email string) {
	switch {
	case strings.TrimSpace(email) == "":
		f.add("email", "email is required")
	case len(email) > maxEmailLength:
		f.add("email", "email is too long")
	default:
		addr, err := mail.ParseAddress(email)
		if err != nil || addr.Address != email {
			f.add("email", "email is not a valid address")
		}
	}
}

func validateFullName(f fieldErrors, fullName string) {
	switch {
	case strings.TrimSpace(fullName) == "":
		f.add("full_name", "full name is required")
	case utf8.RuneCountInString(fullName) > maxFullNameLength:
		f.add("full_name", "full name is too long")
	}
}

// validatePassword applies the same policy the service would, so a bad password
// is reported against its field alongside any other form errors instead of
// arriving alone from a layer below.
func validatePassword(f fieldErrors, field, password string) {
	if password == "" {
		f.add(field, "password is required")
		return
	}
	if err := security.ValidatePassword(password); err != nil {
		f.add(field, err.Error())
	}
}

func validateRequired(f fieldErrors, field, value, message string) {
	if strings.TrimSpace(value) == "" {
		f.add(field, message)
	}
}

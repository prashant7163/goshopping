// Package model holds the domain types the repository, service, and handler
// layers all speak in. The types that appear in responses carry their JSON tags
// here, so the wire shape of a user is defined in exactly one place.
package model

import "time"

// User is a registered account.
//
// PasswordHash is tagged json:"-" rather than left to a separate response DTO:
// the requirement is that no response ever carries the hash, and a struct tag
// enforces that on every path at once, including ones added later.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	FullName     string    `json:"full_name"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

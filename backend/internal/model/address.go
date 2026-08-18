package model

import "time"

// UserAddress is one postal address belonging to a user. At most one per user
// carries IsDefault, enforced by a partial unique index rather than by any code
// path remembering to check (ADR-013).
//
// The table ships in Step 1 and the endpoints in Step 2 (ADR-015), so the JSON
// tags here are the wire shape those endpoints will use.
type UserAddress struct {
	ID     string `json:"id"`
	UserID string `json:"user_id"`

	Street string `json:"street"`
	City   string `json:"city"`
	// State is optional: plenty of countries have no such subdivision, and the
	// column has no not-blank check for that reason.
	State      string `json:"state"`
	PostalCode string `json:"postal_code"`
	Country    string `json:"country"`
	IsDefault  bool   `json:"is_default"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/prashant7163/goshopping/backend/internal/model"
)

// oneDefaultAddressIndex is the partial unique index from migration 000002.
const oneDefaultAddressIndex = "user_addresses_one_default_per_user_idx"

// ErrDefaultAddressExists reports that a write would have given a user a second
// default address. It comes from the index, so it holds under concurrency in a
// way a preceding SELECT would not.
var ErrDefaultAddressExists = errors.New("repository: user already has a default address")

const addressColumns = `id, user_id, street, city, state, postal_code, country,
	is_default, created_at, updated_at`

// UserAddressRepository reads and writes the user_addresses table.
//
// Every method that touches a specific address takes the owning user's id as
// well, so a lookup by a guessed address id returns nothing rather than someone
// else's address. Ownership is a WHERE clause here rather than a check in a
// handler that a later handler might forget.
type UserAddressRepository struct {
	db DBTX
	// inTx records whether this repository is bound to a transaction, which
	// SetDefault requires. See the comment there.
	inTx bool
}

// ListByUser returns a user's addresses, default first and then oldest first,
// which is the order a profile page wants to render.
func (r *UserAddressRepository) ListByUser(ctx context.Context, userID string) ([]model.UserAddress, error) {
	const query = `
		SELECT ` + addressColumns + `
		FROM user_addresses
		WHERE user_id = $1
		ORDER BY is_default DESC, created_at`

	rows, err := r.db.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("listing addresses: %w", err)
	}
	defer rows.Close()

	// Non-nil so an empty result serializes as [] rather than null.
	addresses := make([]model.UserAddress, 0)
	for rows.Next() {
		address, err := scanAddress(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning address: %w", err)
		}
		addresses = append(addresses, *address)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing addresses: %w", err)
	}
	return addresses, nil
}

// ByID returns one address belonging to the given user.
func (r *UserAddressRepository) ByID(ctx context.Context, id, userID string) (*model.UserAddress, error) {
	const query = `SELECT ` + addressColumns + ` FROM user_addresses WHERE id = $1 AND user_id = $2`

	address, err := scanAddress(r.db.QueryRow(ctx, query, id, userID))
	if err != nil {
		return nil, wrapQueryErr("finding address", err)
	}
	return address, nil
}

// CreateAddressParams is one new address. Setting IsDefault requires that the
// user has no other default; see SetDefault for the safe way to move one.
type CreateAddressParams struct {
	UserID     string
	Street     string
	City       string
	State      string
	PostalCode string
	Country    string
	IsDefault  bool
}

// Create inserts an address and returns the stored row.
func (r *UserAddressRepository) Create(ctx context.Context, p CreateAddressParams) (*model.UserAddress, error) {
	const query = `
		INSERT INTO user_addresses (user_id, street, city, state, postal_code, country, is_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING ` + addressColumns

	address, err := scanAddress(r.db.QueryRow(ctx, query,
		p.UserID, p.Street, p.City, p.State, p.PostalCode, p.Country, p.IsDefault))
	if err != nil {
		if isUniqueViolation(err, oneDefaultAddressIndex) {
			return nil, ErrDefaultAddressExists
		}
		return nil, fmt.Errorf("creating address: %w", err)
	}
	return address, nil
}

// UpdateAddressParams replaces an address's fields. IsDefault is absent on
// purpose: moving the default is a two-row operation, and SetDefault owns it.
type UpdateAddressParams struct {
	ID         string
	UserID     string
	Street     string
	City       string
	State      string
	PostalCode string
	Country    string
}

// Update replaces the address's fields, leaving is_default alone. updated_at is
// left to the trigger (ADR-021).
func (r *UserAddressRepository) Update(ctx context.Context, p UpdateAddressParams) (*model.UserAddress, error) {
	const query = `
		UPDATE user_addresses
		SET street = $3, city = $4, state = $5, postal_code = $6, country = $7
		WHERE id = $1 AND user_id = $2
		RETURNING ` + addressColumns

	address, err := scanAddress(r.db.QueryRow(ctx, query,
		p.ID, p.UserID, p.Street, p.City, p.State, p.PostalCode, p.Country))
	if err != nil {
		return nil, wrapQueryErr("updating address", err)
	}
	return address, nil
}

// Delete removes one of the user's addresses. Deleting the default leaves the
// user with none, which the index permits — "at most one", not "exactly one".
func (r *UserAddressRepository) Delete(ctx context.Context, id, userID string) error {
	const query = `DELETE FROM user_addresses WHERE id = $1 AND user_id = $2`

	tag, err := r.db.Exec(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("deleting address: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearDefault unsets the user's default address, if they have one, and reports
// how many rows changed. It is the first half of moving a default and the way to
// make room for a new one created with IsDefault set.
func (r *UserAddressRepository) ClearDefault(ctx context.Context, userID string) (int64, error) {
	const query = `UPDATE user_addresses SET is_default = false WHERE user_id = $1 AND is_default`

	tag, err := r.db.Exec(ctx, query, userID)
	if err != nil {
		return 0, fmt.Errorf("clearing default address: %w", err)
	}
	return tag.RowsAffected(), nil
}

// SetDefault makes one address the user's default, clearing the previous one.
//
// This must be two statements. A single UPDATE flipping both rows at once
// succeeds or fails depending on which row the scan reaches first: if the row
// gaining the flag is written before the row losing it, the index rejects the
// second default. Clearing first is the only order that always works, and the
// two writes have to be atomic — otherwise a failure between them leaves the
// user with no default at all.
//
// Hence the transaction requirement, checked rather than documented and hoped
// for: call this through Store.Tx.
func (r *UserAddressRepository) SetDefault(ctx context.Context, id, userID string) error {
	if !r.inTx {
		return errors.New("repository: SetDefault must be called inside Store.Tx")
	}

	if _, err := r.ClearDefault(ctx, userID); err != nil {
		return err
	}

	const query = `UPDATE user_addresses SET is_default = true WHERE id = $1 AND user_id = $2`

	tag, err := r.db.Exec(ctx, query, id, userID)
	if err != nil {
		return fmt.Errorf("setting default address: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanAddress(row pgx.Row) (*model.UserAddress, error) {
	var a model.UserAddress
	err := row.Scan(
		&a.ID, &a.UserID, &a.Street, &a.City, &a.State, &a.PostalCode,
		&a.Country, &a.IsDefault, &a.CreatedAt, &a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

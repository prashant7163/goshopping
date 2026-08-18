package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/prashant7163/goshopping/backend/internal/model"
)

// usersEmailUniqueConstraint is the name PostgreSQL gives the UNIQUE on
// users.email declared inline in migration 000001.
const usersEmailUniqueConstraint = "users_email_key"

// userColumns is listed explicitly rather than as *, so adding a column does
// not silently break every scan in this file.
const userColumns = `id, email, full_name, password_hash, created_at, updated_at`

// UserRepository reads and writes the users table.
type UserRepository struct {
	db DBTX
}

// Create inserts a user and returns the stored row, including the database's
// generated id and timestamps.
func (r *UserRepository) Create(ctx context.Context, email, fullName, passwordHash string) (*model.User, error) {
	const query = `
		INSERT INTO users (email, full_name, password_hash)
		VALUES ($1, $2, $3)
		RETURNING ` + userColumns

	user, err := scanUser(r.db.QueryRow(ctx, query, email, fullName, passwordHash))
	if err != nil {
		// citext makes this case-insensitive, so User@x.com collides with
		// user@x.com without the application lowercasing anything (ADR-006).
		if isUniqueViolation(err, usersEmailUniqueConstraint) {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("creating user: %w", err)
	}
	return user, nil
}

// ByID returns the user with the given id.
func (r *UserRepository) ByID(ctx context.Context, id string) (*model.User, error) {
	const query = `SELECT ` + userColumns + ` FROM users WHERE id = $1`

	user, err := scanUser(r.db.QueryRow(ctx, query, id))
	if err != nil {
		return nil, wrapQueryErr("finding user by id", err)
	}
	return user, nil
}

// ByIDForUpdate returns the user with the given id and holds a row lock until
// the surrounding transaction ends. A password change reads, checks, and writes
// the same row, and must not interleave with another change doing the same.
func (r *UserRepository) ByIDForUpdate(ctx context.Context, id string) (*model.User, error) {
	const query = `SELECT ` + userColumns + ` FROM users WHERE id = $1 FOR UPDATE`

	user, err := scanUser(r.db.QueryRow(ctx, query, id))
	if err != nil {
		return nil, wrapQueryErr("locking user by id", err)
	}
	return user, nil
}

// ByEmail returns the user with the given email, matched case-insensitively by
// the citext column type.
func (r *UserRepository) ByEmail(ctx context.Context, email string) (*model.User, error) {
	const query = `SELECT ` + userColumns + ` FROM users WHERE email = $1`

	user, err := scanUser(r.db.QueryRow(ctx, query, email))
	if err != nil {
		return nil, wrapQueryErr("finding user by email", err)
	}
	return user, nil
}

// UpdatePassword replaces the stored hash. updated_at is left to the trigger
// (ADR-021).
func (r *UserRepository) UpdatePassword(ctx context.Context, id, passwordHash string) error {
	const query = `UPDATE users SET password_hash = $2 WHERE id = $1`

	tag, err := r.db.Exec(ctx, query, id, passwordHash)
	if err != nil {
		return fmt.Errorf("updating password: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanUser(row pgx.Row) (*model.User, error) {
	var u model.User
	err := row.Scan(&u.ID, &u.Email, &u.FullName, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// wrapQueryErr turns "no rows" into ErrNotFound and annotates everything else
func wrapQueryErr(action string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", action, err)
}

// userColumnsQualified is userColumns with a "u." prefix, for queries that join
// users to another table. Kept next to userColumns so the two lists stay in step.
const userColumnsQualified = `u.id, u.email, u.full_name, u.password_hash, u.created_at, u.updated_at`

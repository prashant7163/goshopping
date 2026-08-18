// Package repository is the only place in the application that speaks SQL
// (ADR-003). Every query lives behind a method here, so a schema change has one
// blast radius, and the service layer can be exercised against fakes.
//
// Repositories translate driver errors into the sentinels below. Callers match
// on those; a *pgconn.PgError never escapes this package.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	// ErrNotFound reports that a query matched no row. Conditional updates
	// return it too, where "no row matched" means the row was not in the state
	// the caller required.
	ErrNotFound = errors.New("repository: record not found")

	// ErrEmailTaken reports that the users email unique constraint rejected a
	// write. It comes from the constraint rather than from a preceding SELECT,
	// so two concurrent signups cannot both pass a check and then both insert.
	ErrEmailTaken = errors.New("repository: email already registered")
)

// DBTX is the part of pgx that both a pool and a transaction satisfy, so every
// query below runs unchanged inside or outside a transaction.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Store bundles the repositories and owns transaction scope. A Store obtained
// from Tx is bound to that transaction, so a service method that must be atomic
// simply uses the Store it was handed.
type Store struct {
	pool *pgxpool.Pool

	Users     *UserRepository
	Sessions  *SessionRepository
	Addresses *UserAddressRepository
}

// New builds a Store over the connection pool.
func New(pool *pgxpool.Pool) *Store {
	return newStore(pool, pool, false)
}

func newStore(pool *pgxpool.Pool, db DBTX, inTx bool) *Store {
	return &Store{
		pool:      pool,
		Users:     &UserRepository{db: db},
		Sessions:  &SessionRepository{db: db},
		Addresses: &UserAddressRepository{db: db, inTx: inTx},
	}
}

// Tx runs fn inside a single transaction, committing when it returns nil and
// rolling back on any error or panic. The Store handed to fn is bound to the
// transaction; the receiver is not, so work must go through the argument.
func (s *Store) Tx(ctx context.Context, fn func(*Store) error) error {
	if s.pool == nil {
		return errors.New("repository: nested transactions are not supported")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	// Rollback after a successful commit is a no-op, so this can be
	// unconditional — which is what makes it cover the panic path too.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(newStore(nil, tx, true)); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// isUniqueViolation reports whether err is a unique constraint failure, for the
// named constraint when one is given.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	if pgErr.Code != pgerrcode.UniqueViolation {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

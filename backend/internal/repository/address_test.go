package repository_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/prashant7163/goshopping/backend/internal/repository"
)

// The address endpoints land in Step 2 (ADR-015), so this test is what exercises
// the repository in the meantime — in particular the rules the partial unique
// index enforces, which no amount of reading the SQL confirms.
//
// It talks to a real PostgreSQL, because what is being checked is PostgreSQL's
// behaviour. It skips when there is none to talk to, so `go test ./...` still
// passes on a machine without Docker running.

const testDatabaseURL = "postgres://goshopping:goshopping@localhost:5432/goshopping?sslmode=disable"

func testStore(t *testing.T) (*repository.Store, *pgxpool.Pool, context.Context) {
	t.Helper()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		url = testDatabaseURL
	}

	ctx := context.Background()
	connectCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	pool, err := pgxpool.New(connectCtx, url)
	if err != nil {
		t.Skipf("no database available: %v", err)
	}
	if err := pool.Ping(connectCtx); err != nil {
		pool.Close()
		t.Skipf("no database available: %v", err)
	}
	t.Cleanup(pool.Close)

	return repository.New(pool), pool, ctx
}

// deleteUser removes an account directly. There is no repository method for it:
// the application never deletes users, and this is only here to keep test data
// from accumulating and to exercise the ON DELETE CASCADE.
func deleteUser(ctx context.Context, pool *pgxpool.Pool, userID string) error {
	_, err := pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	return err
}

// newUser creates a throwaway account and removes it, and everything cascading
// from it, when the test ends.
func newUser(t *testing.T, store *repository.Store, pool *pgxpool.Pool, ctx context.Context) string {
	t.Helper()

	// A syntactically valid bcrypt hash, which the users table checks for. It
	// matches no password, which is the point: nothing can sign in as this user.
	const unusableHash = `$2a$12$` + `0123456789012345678901234567890123456789012345678901x`

	email := t.Name() + "-" + time.Now().Format("150405.000000000") + "@example.test"
	user, err := store.Users.Create(ctx, email, "Address Fixture", unusableHash)
	if err != nil {
		t.Fatalf("creating fixture user: %v", err)
	}

	t.Cleanup(func() {
		if err := deleteUser(ctx, pool, user.ID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})
	return user.ID
}

func address(userID string, street string, isDefault bool) repository.CreateAddressParams {
	return repository.CreateAddressParams{
		UserID:     userID,
		Street:     street,
		City:       "Pune",
		State:      "MH",
		PostalCode: "411001",
		Country:    "IN",
		IsDefault:  isDefault,
	}
}

func TestAddressesListedDefaultFirst(t *testing.T) {
	store, pool, ctx := testStore(t)
	userID := newUser(t, store, pool, ctx)

	if _, err := store.Addresses.Create(ctx, address(userID, "1 First St", false)); err != nil {
		t.Fatalf("creating first address: %v", err)
	}
	second, err := store.Addresses.Create(ctx, address(userID, "2 Second St", true))
	if err != nil {
		t.Fatalf("creating second address: %v", err)
	}

	list, err := store.Addresses.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("listing addresses: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 addresses, got %d", len(list))
	}
	if list[0].ID != second.ID {
		t.Errorf("expected the default address first, got %q", list[0].Street)
	}
}

func TestSecondDefaultAddressRejected(t *testing.T) {
	store, pool, ctx := testStore(t)
	userID := newUser(t, store, pool, ctx)

	if _, err := store.Addresses.Create(ctx, address(userID, "1 First St", true)); err != nil {
		t.Fatalf("creating first address: %v", err)
	}

	_, err := store.Addresses.Create(ctx, address(userID, "2 Second St", true))
	if !errors.Is(err, repository.ErrDefaultAddressExists) {
		t.Fatalf("expected ErrDefaultAddressExists, got %v", err)
	}
}

func TestSetDefaultMovesTheDefault(t *testing.T) {
	store, pool, ctx := testStore(t)
	userID := newUser(t, store, pool, ctx)

	if _, err := store.Addresses.Create(ctx, address(userID, "1 First St", true)); err != nil {
		t.Fatalf("creating first address: %v", err)
	}
	second, err := store.Addresses.Create(ctx, address(userID, "2 Second St", false))
	if err != nil {
		t.Fatalf("creating second address: %v", err)
	}

	// Outside a transaction the two writes could be interrupted between them,
	// so the repository refuses rather than risking a user with no default.
	if err := store.Addresses.SetDefault(ctx, second.ID, userID); err == nil {
		t.Fatal("expected SetDefault outside a transaction to be refused")
	}

	err = store.Tx(ctx, func(tx *repository.Store) error {
		return tx.Addresses.SetDefault(ctx, second.ID, userID)
	})
	if err != nil {
		t.Fatalf("setting default: %v", err)
	}

	list, err := store.Addresses.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("listing addresses: %v", err)
	}
	for _, a := range list {
		want := a.ID == second.ID
		if a.IsDefault != want {
			t.Errorf("address %q: is_default = %v, want %v", a.Street, a.IsDefault, want)
		}
	}
	if list[0].ID != second.ID {
		t.Errorf("expected the new default first in the list")
	}
}

func TestAddressesAreScopedToTheirOwner(t *testing.T) {
	store, pool, ctx := testStore(t)
	ownerID := newUser(t, store, pool, ctx)
	strangerID := newUser(t, store, pool, ctx)

	owned, err := store.Addresses.Create(ctx, address(ownerID, "1 First St", true))
	if err != nil {
		t.Fatalf("creating address: %v", err)
	}

	if _, err := store.Addresses.ByID(ctx, owned.ID, strangerID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("ByID for a stranger: expected ErrNotFound, got %v", err)
	}
	if err := store.Addresses.Delete(ctx, owned.ID, strangerID); !errors.Is(err, repository.ErrNotFound) {
		t.Errorf("Delete for a stranger: expected ErrNotFound, got %v", err)
	}

	list, err := store.Addresses.ListByUser(ctx, strangerID)
	if err != nil {
		t.Fatalf("listing addresses: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected the stranger to have no addresses, got %d", len(list))
	}

	// The owner can still reach it, so the scoping rejected the caller rather
	// than the address.
	if _, err := store.Addresses.ByID(ctx, owned.ID, ownerID); err != nil {
		t.Errorf("owner cannot read their own address: %v", err)
	}
}

func TestUpdateLeavesTheDefaultFlagAlone(t *testing.T) {
	store, pool, ctx := testStore(t)
	userID := newUser(t, store, pool, ctx)

	created, err := store.Addresses.Create(ctx, address(userID, "1 First St", true))
	if err != nil {
		t.Fatalf("creating address: %v", err)
	}

	updated, err := store.Addresses.Update(ctx, repository.UpdateAddressParams{
		ID:         created.ID,
		UserID:     userID,
		Street:     "3 Third St",
		City:       "Mumbai",
		State:      "",
		PostalCode: "400001",
		Country:    "IN",
	})
	if err != nil {
		t.Fatalf("updating address: %v", err)
	}

	if updated.Street != "3 Third St" || updated.City != "Mumbai" {
		t.Errorf("update did not take: %+v", updated)
	}
	if !updated.IsDefault {
		t.Error("update cleared is_default, which it must leave to SetDefault")
	}
	// The trigger maintains this, not the UPDATE above (ADR-021).
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("updated_at did not advance: %v then %v", created.UpdatedAt, updated.UpdatedAt)
	}
}

func TestDeletingAUserRemovesTheirAddresses(t *testing.T) {
	store, pool, ctx := testStore(t)
	userID := newUser(t, store, pool, ctx)

	if _, err := store.Addresses.Create(ctx, address(userID, "1 First St", true)); err != nil {
		t.Fatalf("creating address: %v", err)
	}
	if err := deleteUser(ctx, pool, userID); err != nil {
		t.Fatalf("deleting user: %v", err)
	}

	list, err := store.Addresses.ListByUser(ctx, userID)
	if err != nil {
		t.Fatalf("listing addresses: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected the cascade to remove %d address(es)", len(list))
	}
}

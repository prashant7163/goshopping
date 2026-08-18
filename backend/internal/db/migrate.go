package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"strings"

	"github.com/golang-migrate/migrate/v4"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
)

// NoVersion is reported by Version for a database with nothing applied yet.
const NoVersion = uint(0)

// Migrator applies the schema migrations. It holds its own database/sql
// connection because golang-migrate is built on that interface; the pool the
// rest of the application uses is untouched.
//
// Migrations take a Postgres advisory lock for their duration, so several
// instances booting at once queue rather than collide.
type Migrator struct {
	m *migrate.Migrate
}

// NewMigrator prepares a migrator over the embedded migration files. Close it
// when done: it owns a connection.
func NewMigrator(databaseURL string, files fs.FS, logger *slog.Logger) (*Migrator, error) {
	source, err := iofs.New(files, ".")
	if err != nil {
		return nil, fmt.Errorf("reading migrations: %w", err)
	}

	connCfg, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parsing DATABASE_URL: %w", err)
	}

	// stdlib over the pgx driver already in use, rather than golang-migrate's
	// URL-based constructor, which would pull in a second Postgres driver.
	sqlDB := stdlib.OpenDB(*connCfg)

	driver, err := migratepgx.WithInstance(sqlDB, &migratepgx.Config{})
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("preparing migration driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("preparing migrator: %w", err)
	}
	if logger != nil {
		m.Log = migrateLogger{logger: logger}
	}

	return &Migrator{m: m}, nil
}

// Close releases the migrator's connection.
func (mg *Migrator) Close() error {
	sourceErr, dbErr := mg.m.Close()
	return errors.Join(sourceErr, dbErr)
}

// Up applies every pending migration and reports whether anything changed.
func (mg *Migrator) Up() (bool, error) {
	err := mg.m.Up()
	if errors.Is(err, migrate.ErrNoChange) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("applying migrations: %w", err)
	}
	return true, nil
}

// Down rolls back the given number of migrations, or all of them when steps is
// zero or less.
func (mg *Migrator) Down(steps int) (bool, error) {
	var err error
	if steps <= 0 {
		err = mg.m.Down()
	} else {
		err = mg.m.Steps(-steps)
	}

	// ErrNilVersion means there was nothing left to roll back.
	if errors.Is(err, migrate.ErrNoChange) || errors.Is(err, migrate.ErrNilVersion) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("rolling back migrations: %w", err)
	}
	return true, nil
}

// Version reports the applied version and whether the database is dirty. A
// dirty database is one whose last migration failed partway; it must be
// repaired by hand and then cleared with Force.
func (mg *Migrator) Version() (version uint, dirty bool, err error) {
	version, dirty, err = mg.m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return NoVersion, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("reading schema version: %w", err)
	}
	return version, dirty, nil
}

// Force records version as applied and clears the dirty flag, without running
// any SQL. It is the escape hatch after a failed migration has been fixed by
// hand.
func (mg *Migrator) Force(version int) error {
	if err := mg.m.Force(version); err != nil {
		return fmt.Errorf("forcing version %d: %w", version, err)
	}
	return nil
}

// Migrate applies pending migrations and logs the resulting schema version. It
// is the one-shot form used at startup.
func Migrate(databaseURL string, files fs.FS, logger *slog.Logger) error {
	mg, err := NewMigrator(databaseURL, files, logger)
	if err != nil {
		return err
	}
	defer mg.Close()

	changed, err := mg.Up()
	if err != nil {
		return err
	}

	version, dirty, err := mg.Version()
	if err != nil {
		return err
	}

	logger.Info("migrations applied",
		slog.Uint64("version", uint64(version)),
		slog.Bool("changed", changed),
		slog.Bool("dirty", dirty),
	)
	return nil
}

// migrateLogger routes golang-migrate's output through slog, so migration lines
// share the format and destination of everything else (ADR-017).
type migrateLogger struct {
	logger *slog.Logger
}

func (l migrateLogger) Printf(format string, v ...any) {
	l.logger.Debug(strings.TrimSpace(fmt.Sprintf(format, v...)))
}

// Verbose reports whether golang-migrate should emit its per-step lines. They
// are logged at debug, so the handler's level decides whether they survive.
func (l migrateLogger) Verbose() bool {
	return l.logger.Enabled(context.Background(), slog.LevelDebug)
}

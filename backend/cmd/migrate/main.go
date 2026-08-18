// Command migrate applies and rolls back the database schema migrations. The
// server also migrates on boot in development; this is the form used when
// migrating is a deliberate step, or when a failed migration needs repairing.
//
// Usage:
//
//	migrate up            apply every pending migration
//	migrate down [n]      roll back n migrations (default 1)
//	migrate down all      roll back every migration
//	migrate version       print the applied version
//	migrate force <n>     record version n and clear the dirty flag
package main

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/prashant7163/goshopping/backend/internal/config"
	"github.com/prashant7163/goshopping/backend/internal/db"
	"github.com/prashant7163/goshopping/backend/migrations"
)

const usage = `usage: migrate <command>

  up            apply every pending migration
  down [n]      roll back n migrations (default 1)
  down all      roll back every migration
  version       print the applied version
  force <n>     record version n and clear the dirty flag
`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("no command given")
	}

	// Same candidates as the server, so the command works from ./backend or
	// from the repository root.
	cfg, err := config.Load(".env", "backend/.env")
	if err != nil {
		return err
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	mg, err := db.NewMigrator(cfg.DatabaseURL, migrations.FS, logger)
	if err != nil {
		return err
	}
	defer mg.Close()

	switch cmd := args[0]; cmd {
	case "up":
		return up(mg, logger)
	case "down":
		return down(mg, logger, args[1:])
	case "version":
		return version(mg, logger)
	case "force":
		return force(mg, logger, args[1:])
	default:
		fmt.Fprint(os.Stderr, usage)
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func up(mg *db.Migrator, logger *slog.Logger) error {
	changed, err := mg.Up()
	if err != nil {
		return err
	}
	if !changed {
		logger.Info("no pending migrations")
	}
	return version(mg, logger)
}

func down(mg *db.Migrator, logger *slog.Logger, args []string) error {
	steps := 1
	if len(args) > 0 {
		if args[0] == "all" {
			steps = 0
		} else {
			n, err := strconv.Atoi(args[0])
			if err != nil || n < 1 {
				return fmt.Errorf(`down takes a positive number of steps or "all" (got %q)`, args[0])
			}
			steps = n
		}
	}

	changed, err := mg.Down(steps)
	if err != nil {
		return err
	}
	if !changed {
		logger.Info("nothing to roll back")
	}
	return version(mg, logger)
}

func version(mg *db.Migrator, logger *slog.Logger) error {
	v, dirty, err := mg.Version()
	if err != nil {
		return err
	}
	if v == db.NoVersion {
		logger.Info("no migrations applied")
		return nil
	}

	logger.Info("schema version", slog.Uint64("version", uint64(v)), slog.Bool("dirty", dirty))
	if dirty {
		// Left behind by a migration that failed partway. golang-migrate
		// refuses to move until a human has confirmed the actual state.
		return fmt.Errorf("database is dirty at version %d: repair the schema by hand, then run `migrate force %d`", v, v)
	}
	return nil
}

func force(mg *db.Migrator, logger *slog.Logger, args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("force takes exactly one version")
	}
	v, err := strconv.Atoi(args[0])
	if err != nil || v < 0 {
		return fmt.Errorf("force takes a non-negative version (got %q)", args[0])
	}

	if err := mg.Force(v); err != nil {
		return err
	}
	logger.Info("version forced", slog.Int("version", v))
	return nil
}

// Command server runs the GoShopping HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/prashant7163/goshopping/backend/internal/apierror"
	"github.com/prashant7163/goshopping/backend/internal/config"
	"github.com/prashant7163/goshopping/backend/internal/db"
	"github.com/prashant7163/goshopping/backend/internal/handler"
	appmw "github.com/prashant7163/goshopping/backend/internal/middleware"
	"github.com/prashant7163/goshopping/backend/internal/repository"
	"github.com/prashant7163/goshopping/backend/internal/security"
	"github.com/prashant7163/goshopping/backend/internal/service"
	"github.com/prashant7163/goshopping/backend/migrations"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Both candidates are tried so the server starts whether it is run from
	// ./backend or from the repository root.
	cfg, err := config.Load(".env", "backend/.env")
	if err != nil {
		return err
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	// Cancelled on SIGINT/SIGTERM, which begins graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBMinConns)
	if err != nil {
		return err
	}
	defer pool.Close()
	logger.Info("database connected",
		slog.Int("max_conns", int(cfg.DBMaxConns)),
		slog.Int("min_conns", int(cfg.DBMinConns)),
	)

	// Before the first request, so a half-migrated schema is a startup failure
	// rather than a runtime one.
	if cfg.DBAutoMigrate {
		if err := db.Migrate(cfg.DatabaseURL, migrations.FS, logger); err != nil {
			return err
		}
	} else {
		logger.Info("automatic migrations disabled", slog.String("env", cfg.AppEnv))
	}

	store := repository.New(pool)
	auth := service.NewAuth(
		store,
		security.NewHasher(cfg.BcryptCost),
		security.NewTokens(cfg.JWTSecret, cfg.JWTAccessTTL, cfg.JWTRefreshTTL),
		logger,
	)

	// Started before the server so a process that never serves a request still
	// clears the table, and stopped before the pool closes: the returned func is
	// deferred after pool.Close, and deferred calls run last in, first out.
	stopReaper := service.NewReaper(store, cfg.SessionReapInterval, cfg.SessionReapGrace, logger).Start(ctx)
	defer stopReaper()

	e := newEcho(cfg, logger)
	registerRoutes(e, pool, auth)

	srvErr := make(chan error, 1)
	go func() {
		logger.Info("server listening",
			slog.String("addr", cfg.Addr()),
			slog.String("env", cfg.AppEnv),
			slog.Any("cors_origins", cfg.CORSAllowedOrigins),
		)
		if err := e.Start(cfg.Addr()); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- fmt.Errorf("starting server: %w", err)
		}
		close(srvErr)
	}()

	select {
	case err := <-srvErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received", slog.Duration("timeout", cfg.ShutdownTimeout))
	}

	// A fresh context: the signal-derived one is already cancelled, and
	// in-flight requests need a window to finish.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := e.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	logger.Info("server stopped")
	return nil
}

func newEcho(cfg config.Config, logger *slog.Logger) *echo.Echo {
	e := echo.New()
	e.HideBanner = true
	// Startup is announced through slog instead, so logs stay in one format.
	e.HidePort = true

	e.HTTPErrorHandler = apierror.Handler(logger)

	// Bounds on slow or idle clients; without these a connection can be held open
	// indefinitely.
	e.Server.ReadHeaderTimeout = 10 * time.Second
	e.Server.ReadTimeout = 30 * time.Second
	e.Server.WriteTimeout = 30 * time.Second
	e.Server.IdleTimeout = 60 * time.Second

	// Order matters: request IDs must exist before the logger reads them, and
	// recovery must wrap everything downstream of it.
	e.Use(appmw.RequestID())
	e.Use(appmw.RequestLogger(logger))
	e.Use(appmw.Recover())
	e.Use(appmw.CORS(cfg.CORSAllowedOrigins))

	return e
}

func registerRoutes(e *echo.Echo, pool *pgxpool.Pool, auth *service.Auth) {
	health := handler.NewHealth(pool)
	e.GET("/healthz", health.Check)

	requireAuth := appmw.RequireAuth(auth)

	authHandler := handler.NewAuth(auth)
	userHandler := handler.NewUser(auth)

	v1 := e.Group("/api/v1")

	// Signup, login, and refresh are unauthenticated: each one is how a client
	// obtains the credential the others require. Logout is not — it revokes the
	// session the caller presents.
	authRoutes := v1.Group("/auth")
	authRoutes.POST("/signup", authHandler.Signup)
	authRoutes.POST("/login", authHandler.Login)
	authRoutes.POST("/refresh", authHandler.Refresh)
	authRoutes.POST("/logout", authHandler.Logout, requireAuth)

	userRoutes := v1.Group("/users", requireAuth)
	userRoutes.GET("/me", userHandler.Me)
	userRoutes.PATCH("/me/password", userHandler.ChangePassword)
}

func newLogger(cfg config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{Level: level}

	// Text is readable in a terminal; JSON is what log aggregators expect.
	if cfg.IsDevelopment() {
		return slog.New(slog.NewTextHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

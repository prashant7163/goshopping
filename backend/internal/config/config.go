// Package config resolves every runtime setting from the environment once, at
// startup. A misconfigured process fails immediately with the full list of
// problems rather than surfacing them one at a time on first use.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// devJWTSecret is the placeholder shipped in .env.example. Booting with it
// outside development is refused rather than merely warned about.
const devJWTSecret = "dev-only-insecure-secret-replace-me"

// minProdSecretLen is the shortest JWT signing secret accepted outside
// development. HS256 keys shorter than the hash output weaken the signature.
const minProdSecretLen = 32

type Config struct {
	AppEnv          string
	ServerPort      int
	ShutdownTimeout time.Duration
	LogLevel        string

	DatabaseURL   string
	DBMaxConns    int32
	DBMinConns    int32
	DBAutoMigrate bool

	JWTSecret     string
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
	BcryptCost    int

	SessionReapInterval time.Duration
	SessionReapGrace    time.Duration

	CORSAllowedOrigins []string
}

func (c Config) IsDevelopment() bool { return c.AppEnv == envDevelopment }

// Addr renders the listen address for the HTTP server.
func (c Config) Addr() string { return fmt.Sprintf(":%d", c.ServerPort) }

const (
	envDevelopment = "development"
	envTest        = "test"
	envProduction  = "production"
)

// Load reads the first existing file from envFiles, then resolves configuration
// from the process environment. Variables already set in the environment always
// win: the files only fill gaps, so a shell override beats a stale .env.
func Load(envFiles ...string) (Config, error) {
	for _, f := range envFiles {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		if err := godotenv.Load(f); err != nil {
			return Config{}, fmt.Errorf("loading %s: %w", f, err)
		}
	}

	var l loader

	// Read first: it decides the default for DB_AUTO_MIGRATE below.
	appEnv := l.text("APP_ENV", envDevelopment)

	cfg := Config{
		AppEnv:          appEnv,
		ServerPort:      l.number("SERVER_PORT", 8080),
		ShutdownTimeout: l.duration("SERVER_SHUTDOWN_TIMEOUT", 10*time.Second),
		LogLevel:        l.text("LOG_LEVEL", "info"),

		DatabaseURL: l.text("DATABASE_URL", ""),
		DBMaxConns:  int32(l.number("DB_MAX_CONNS", 10)),
		DBMinConns:  int32(l.number("DB_MIN_CONNS", 2)),
		// Convenient in development, where the schema changes often. Elsewhere
		// migrating is a deliberate deploy step, run before the new binary
		// starts serving (ADR-019).
		DBAutoMigrate: l.boolean("DB_AUTO_MIGRATE", appEnv == envDevelopment),

		JWTSecret:     l.text("JWT_SECRET", ""),
		JWTAccessTTL:  l.duration("JWT_ACCESS_TTL", 24*time.Hour),
		JWTRefreshTTL: l.duration("JWT_REFRESH_TTL", 720*time.Hour),
		BcryptCost:    l.number("BCRYPT_COST", 12),

		SessionReapInterval: l.duration("SESSION_REAP_INTERVAL", 24*time.Hour),
		SessionReapGrace:    l.duration("SESSION_REAP_GRACE", 168*time.Hour),

		CORSAllowedOrigins: l.list("CORS_ALLOWED_ORIGINS", "http://localhost:5173"),
	}

	cfg.validate(&l)

	if len(l.problems) > 0 {
		return Config{}, fmt.Errorf(
			"invalid configuration:\n  - %s",
			strings.Join(l.problems, "\n  - "),
		)
	}
	return cfg, nil
}

func (c Config) validate(l *loader) {
	switch c.AppEnv {
	case envDevelopment, envTest, envProduction:
	default:
		l.problemf("APP_ENV must be one of development, test, production (got %q)", c.AppEnv)
	}

	switch c.LogLevel {
	case "debug", "info", "warn", "error":
	default:
		l.problemf("LOG_LEVEL must be one of debug, info, warn, error (got %q)", c.LogLevel)
	}

	if c.ServerPort < 1 || c.ServerPort > 65535 {
		l.problemf("SERVER_PORT must be between 1 and 65535 (got %d)", c.ServerPort)
	}
	if c.ShutdownTimeout <= 0 {
		l.problem("SERVER_SHUTDOWN_TIMEOUT must be positive")
	}

	if c.DatabaseURL == "" {
		l.problem("DATABASE_URL is required")
	}
	if c.DBMaxConns < 1 {
		l.problemf("DB_MAX_CONNS must be at least 1 (got %d)", c.DBMaxConns)
	}
	if c.DBMinConns < 0 {
		l.problemf("DB_MIN_CONNS cannot be negative (got %d)", c.DBMinConns)
	}
	if c.DBMinConns > c.DBMaxConns {
		l.problemf("DB_MIN_CONNS (%d) cannot exceed DB_MAX_CONNS (%d)", c.DBMinConns, c.DBMaxConns)
	}

	switch {
	case c.JWTSecret == "":
		l.problem("JWT_SECRET is required")
	case c.AppEnv == envProduction && c.JWTSecret == devJWTSecret:
		l.problem("JWT_SECRET is still the development placeholder; generate one with `openssl rand -base64 48`")
	case c.AppEnv == envProduction && len(c.JWTSecret) < minProdSecretLen:
		l.problemf("JWT_SECRET must be at least %d characters outside development (got %d)", minProdSecretLen, len(c.JWTSecret))
	}

	if c.JWTAccessTTL <= 0 {
		l.problem("JWT_ACCESS_TTL must be positive")
	}
	if c.JWTRefreshTTL <= 0 {
		l.problem("JWT_REFRESH_TTL must be positive")
	}
	// A refresh token that dies before its access token cannot renew anything.
	if c.JWTAccessTTL > 0 && c.JWTRefreshTTL > 0 && c.JWTRefreshTTL < c.JWTAccessTTL {
		l.problemf("JWT_REFRESH_TTL (%s) must be at least JWT_ACCESS_TTL (%s)", c.JWTRefreshTTL, c.JWTAccessTTL)
	}

	// Bounds imposed by the bcrypt implementation itself.
	if c.BcryptCost < 4 || c.BcryptCost > 31 {
		l.problemf("BCRYPT_COST must be between 4 and 31 (got %d)", c.BcryptCost)
	}

	if c.SessionReapInterval <= 0 {
		l.problem("SESSION_REAP_INTERVAL must be positive")
	}
	if c.SessionReapGrace < 0 {
		l.problem("SESSION_REAP_GRACE cannot be negative")
	}

	if len(c.CORSAllowedOrigins) == 0 {
		l.problem("CORS_ALLOWED_ORIGINS must list at least one origin")
	}
	for _, o := range c.CORSAllowedOrigins {
		if o == "*" {
			l.problem(`CORS_ALLOWED_ORIGINS must not be "*"; list explicit origins`)
		}
	}
}

// loader reads typed values from the environment, collecting every parse
// failure so Load can report all of them together.
type loader struct {
	problems []string
}

func (l *loader) problem(msg string) { l.problems = append(l.problems, msg) }

func (l *loader) problemf(format string, args ...any) {
	l.problems = append(l.problems, fmt.Sprintf(format, args...))
}

func (l *loader) text(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func (l *loader) number(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		l.problemf("%s must be an integer (got %q)", key, raw)
		return fallback
	}
	return v
}

func (l *loader) boolean(key string, fallback bool) bool {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := strconv.ParseBool(raw)
	if err != nil {
		l.problemf("%s must be a boolean such as true or false (got %q)", key, raw)
		return fallback
	}
	return v
}

func (l *loader) duration(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	v, err := time.ParseDuration(raw)
	if err != nil {
		l.problemf("%s must be a duration such as 24h or 30m (got %q)", key, raw)
		return fallback
	}
	return v
}

func (l *loader) list(key, fallback string) []string {
	raw := l.text(key, fallback)
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

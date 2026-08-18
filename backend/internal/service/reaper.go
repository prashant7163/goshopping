package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/prashant7163/goshopping/backend/internal/repository"
)

// sweepTimeout bounds one delete, so a slow sweep cannot hold a pool connection
// until the next tick.
const sweepTimeout = 30 * time.Second

// Reaper deletes session rows that stopped mattering. Nothing depends on it for
// correctness — an expired session already fails IsActive — it exists so the
// table the middleware searches does not grow without limit.
type Reaper struct {
	store    *repository.Store
	interval time.Duration
	grace    time.Duration
	logger   *slog.Logger
	now      func() time.Time
}

// NewReaper returns a reaper that sweeps every interval, deleting rows that
// died more than grace ago. The grace period keeps recently expired sessions
// readable, which is the difference between diagnosing "your session expired"
// and finding nothing at all.
func NewReaper(store *repository.Store, interval, grace time.Duration, logger *slog.Logger) *Reaper {
	return &Reaper{store: store, interval: interval, grace: grace, logger: logger, now: time.Now}
}

// Start sweeps once and then on every tick, in its own goroutine. The returned
// function stops the loop and blocks until it has exited, so the caller can
// close the connection pool knowing nothing is still using it.
func (r *Reaper) Start(parent context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})

	go func() {
		defer close(done)

		// Immediately, not after the first interval: with a 24h interval, a
		// process that restarts daily would otherwise never sweep at all.
		r.sweep(ctx)

		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				r.sweep(ctx)
			}
		}
	}()

	r.logger.Info("session reaper started",
		slog.Duration("interval", r.interval),
		slog.Duration("grace", r.grace),
	)

	return func() {
		cancel()
		<-done
		r.logger.Info("session reaper stopped")
	}
}

func (r *Reaper) sweep(ctx context.Context) {
	sweepCtx, cancel := context.WithTimeout(ctx, sweepTimeout)
	defer cancel()

	cutoff := r.now().Add(-r.grace)

	deleted, err := r.store.Sessions.DeleteReapable(sweepCtx, cutoff)
	if err != nil {
		// Shutdown cancelled the sweep; that is not a failure worth reporting.
		if errors.Is(err, context.Canceled) {
			return
		}
		r.logger.ErrorContext(ctx, "reaping sessions failed", slog.String("error", err.Error()))
		return
	}

	if deleted > 0 {
		r.logger.InfoContext(ctx, "expired sessions reaped",
			slog.Int64("deleted", deleted),
			slog.Time("cutoff", cutoff),
		)
	}
}

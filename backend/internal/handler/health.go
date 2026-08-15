// Package handler contains the HTTP handlers. Handlers parse input, delegate to
// a service, and render a response; business rules live in the service layer.
package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"
)

// dbCheckTimeout keeps a slow database from stalling the health endpoint, which
// is often polled by an orchestrator on a short interval.
const dbCheckTimeout = 2 * time.Second

type Health struct {
	pool *pgxpool.Pool
}

func NewHealth(pool *pgxpool.Pool) *Health {
	return &Health{pool: pool}
}

type healthResponse struct {
	Status   string `json:"status"`
	Database string `json:"database"`
}

// Check reports process and database health.
//
//	GET /healthz -> 200 when the database answers, 503 when it does not.
func (h *Health) Check(c echo.Context) error {
	ctx, cancel := context.WithTimeout(c.Request().Context(), dbCheckTimeout)
	defer cancel()

	if err := h.pool.Ping(ctx); err != nil {
		// Deliberately not an error return: this is a successful report of a
		// degraded state, and the error handler would mask the detail.
		return c.JSON(http.StatusServiceUnavailable, healthResponse{
			Status:   "degraded",
			Database: "down",
		})
	}

	return c.JSON(http.StatusOK, healthResponse{
		Status:   "ok",
		Database: "up",
	})
}

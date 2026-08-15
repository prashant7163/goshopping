// Package middleware holds this application's HTTP middleware and the
// configuration of Echo's built-ins.
package middleware

import (
	"log/slog"
	"net/http"

	"github.com/labstack/echo/v4"
	echomw "github.com/labstack/echo/v4/middleware"
)

// RequestLogger bridges Echo's request logger into slog so application logs and
// request logs share one format and destination.
func RequestLogger(logger *slog.Logger) echo.MiddlewareFunc {
	return echomw.RequestLoggerWithConfig(echomw.RequestLoggerConfig{
		LogStatus:    true,
		LogMethod:    true,
		LogURI:       true,
		LogLatency:   true,
		LogRemoteIP:  true,
		LogRequestID: true,
		LogError:     true,
		// Let the central error handler run first so the status logged is the
		// one actually sent, not the zero value.
		HandleError: true,
		LogValuesFunc: func(c echo.Context, v echomw.RequestLoggerValues) error {
			attrs := []slog.Attr{
				slog.String("method", v.Method),
				slog.String("uri", v.URI),
				slog.Int("status", v.Status),
				slog.Duration("latency", v.Latency),
				slog.String("remote_ip", v.RemoteIP),
				slog.String("request_id", v.RequestID),
			}
			if v.Error != nil {
				attrs = append(attrs, slog.String("error", v.Error.Error()))
			}

			level := slog.LevelInfo
			switch {
			case v.Status >= http.StatusInternalServerError:
				level = slog.LevelError
			case v.Status >= http.StatusBadRequest:
				level = slog.LevelWarn
			}

			logger.LogAttrs(c.Request().Context(), level, "request", attrs...)
			return nil
		},
	})
}

// CORS permits the configured frontend origins. Credentials stay off because
// tokens travel in the Authorization header rather than cookies (ADR-011); if
// the refresh token ever moves to an httpOnly cookie, this must change.
func CORS(origins []string) echo.MiddlewareFunc {
	return echomw.CORSWithConfig(echomw.CORSConfig{
		AllowOrigins: origins,
		AllowMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPatch,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowHeaders: []string{
			echo.HeaderOrigin,
			echo.HeaderContentType,
			echo.HeaderAccept,
			echo.HeaderAuthorization,
		},
		AllowCredentials: false,
		MaxAge:           300,
	})
}

// RequestID assigns each request an identifier, echoed back in the response
// header and attached to every log line for that request.
func RequestID() echo.MiddlewareFunc {
	return echomw.RequestID()
}

// Recover converts a panic into a 500 through the central error handler instead
// of killing the process.
func Recover() echo.MiddlewareFunc {
	return echomw.RecoverWithConfig(echomw.RecoverConfig{
		DisablePrintStack: false,
	})
}

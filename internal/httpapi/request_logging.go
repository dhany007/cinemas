package httpapi

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/labstack/echo/v4"
)

func (s *Server) requestLogger(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		startedAt := time.Now()
		err := next(c)
		status := c.Response().Status
		s.logger.Info("HTTP request completed",
			"request_id", c.Response().Header().Get(echo.HeaderXRequestID),
			"method", c.Request().Method,
			"route", c.Path(),
			"status", status,
			"outcome", requestOutcome(status),
			"duration_ms", time.Since(startedAt).Milliseconds(),
		)
		return err
	}
}

func requestOutcome(status int) string {
	if status >= http.StatusInternalServerError {
		return "server_error"
	}
	if status >= http.StatusBadRequest {
		return "client_error"
	}
	return "success"
}

func defaultLogger() *slog.Logger {
	return slog.Default()
}

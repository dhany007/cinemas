package httpapi

import (
	"errors"
	"net/http"

	"github.com/citradigital/cinemas/internal/booking"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Server exposes the booking HTTP API.
type Server struct {
	e *echo.Echo
}

// NewServer creates an Echo server backed by the booking service.
func NewServer(bookingService *booking.Service) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())

	server := &Server{e: e}
	e.GET("/healthz", server.health)
	e.POST("/v1/orders", server.createOrderHold(bookingService))
	return server
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.e.ServeHTTP(writer, request)
}

type createOrderRequest struct {
	UserID     string   `json:"user_id"`
	ShowtimeID string   `json:"showtime_id"`
	SeatIDs    []string `json:"seat_ids"`
}

type orderResponse struct {
	ID        string              `json:"id"`
	Status    booking.OrderStatus `json:"status"`
	ExpiresAt string              `json:"expires_at"`
	SeatIDs   []string            `json:"seat_ids"`
}

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

func (s *Server) health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createOrderHold(service *booking.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		idempotencyKey := c.Request().Header.Get("Idempotency-Key")
		if idempotencyKey == "" {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "Idempotency-Key is required")
		}

		var request createOrderRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}

		order, err := service.CreateHold(c.Request().Context(), booking.CreateHoldInput{
			UserID:         request.UserID,
			ShowtimeID:     request.ShowtimeID,
			SeatIDs:        request.SeatIDs,
			IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			return writeBookingError(c, err)
		}

		seatIDs := make([]string, len(order.Items))
		for i, item := range order.Items {
			seatIDs[i] = item.SeatID
		}
		return c.JSON(http.StatusCreated, orderResponse{
			ID:        order.ID,
			Status:    order.Status,
			ExpiresAt: order.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			SeatIDs:   seatIDs,
		})
	}
}

func writeBookingError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, booking.ErrInvalidHoldInput):
		return writeError(
			c,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"user, showtime, and at least one unique seat are required",
		)
	case errors.Is(err, booking.ErrSeatUnavailable):
		return writeError(c, http.StatusConflict, "SEAT_UNAVAILABLE", "one or more selected seats are no longer available")
	case errors.Is(err, booking.ErrIdempotencyKeyReused):
		return writeError(
			c,
			http.StatusConflict,
			"IDEMPOTENCY_KEY_REUSED",
			"Idempotency-Key was used with a different request",
		)
	default:
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to create order")
	}
}

func writeError(c echo.Context, status int, code, message string) error {
	return c.JSON(status, errorResponse{
		Code:      code,
		Message:   message,
		RequestID: c.Response().Header().Get(echo.HeaderXRequestID),
	})
}

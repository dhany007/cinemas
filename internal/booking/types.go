package booking

import (
	"errors"
	"time"
)

var (
	ErrInvalidHoldInput     = errors.New("invalid seat hold input")
	ErrSeatUnavailable      = errors.New("seat unavailable")
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with different request")
)

type SeatStatus string

const (
	SeatAvailable SeatStatus = "AVAILABLE"
	SeatHeld      SeatStatus = "HELD"
	SeatSold      SeatStatus = "SOLD"
)

type OrderStatus string

const (
	OrderPendingPayment OrderStatus = "PENDING_PAYMENT"
)

type Seat struct {
	ID            string
	ShowtimeID    string
	Status        SeatStatus
	HoldOrderID   string
	HoldExpiresAt time.Time
}

type OrderItem struct {
	SeatID string
}

type Order struct {
	ID             string
	UserID         string
	ShowtimeID     string
	IdempotencyKey string
	Status         OrderStatus
	ExpiresAt      time.Time
	Items          []OrderItem
}

type CreateHoldInput struct {
	UserID         string
	ShowtimeID     string
	SeatIDs        []string
	IdempotencyKey string
}

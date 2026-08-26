package booking

import (
	"errors"
	"time"
)

var (
	// ErrInvalidHoldInput indicates a missing or malformed seat-hold request.
	ErrInvalidHoldInput = errors.New("invalid seat hold input")
	// ErrSeatUnavailable indicates that at least one requested seat cannot be held.
	ErrSeatUnavailable = errors.New("seat unavailable")
	// ErrIdempotencyKeyReused indicates the same key was sent with a different request.
	ErrIdempotencyKeyReused = errors.New("idempotency key reused with different request")
)

// SeatStatus describes a seat's availability for one showtime.
type SeatStatus string

const (
	// SeatAvailable means no active hold or sale exists for the seat.
	SeatAvailable SeatStatus = "AVAILABLE"
	// SeatHeld means the seat is reserved until its hold expiry.
	SeatHeld SeatStatus = "HELD"
	// SeatSold means the seat is no longer available for purchase.
	SeatSold SeatStatus = "SOLD"
)

// OrderStatus describes the lifecycle state of a ticket order.
type OrderStatus string

const (
	// OrderPendingPayment means the selected seats are held awaiting payment.
	OrderPendingPayment OrderStatus = "PENDING_PAYMENT"
)

// Seat is the inventory state of a seat for a specific showtime.
type Seat struct {
	ID            string
	ShowtimeID    string
	Status        SeatStatus
	HoldOrderID   string
	HoldExpiresAt time.Time
}

// OrderItem records a single held seat in an order.
type OrderItem struct {
	SeatID string
}

// Order is a pending or completed ticket booking.
type Order struct {
	ID             string
	UserID         string
	ShowtimeID     string
	IdempotencyKey string
	Status         OrderStatus
	ExpiresAt      time.Time
	Items          []OrderItem
}

// CreateHoldInput contains the caller-supplied values for a seat hold.
type CreateHoldInput struct {
	UserID         string
	ShowtimeID     string
	SeatIDs        []string
	IdempotencyKey string
}

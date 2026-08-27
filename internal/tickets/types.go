package tickets

import (
	"context"
	"errors"
	"time"
)

var (
	ErrOrderNotFound = errors.New("order not found")
)

type TicketStatus string

const (
	TicketIssued TicketStatus = "ISSUED"
)

type DeliveryStatus string

const (
	DeliveryPending    DeliveryStatus = "PENDING"
	DeliveryProcessing DeliveryStatus = "PROCESSING"
	DeliveryCompleted  DeliveryStatus = "COMPLETED"
)

// Ticket contains only values that may be returned to its owner. TokenHash is
// intentionally never populated by production repositories.
type Ticket struct {
	ID        string
	OrderID   string
	UserID    string
	Code      string
	QRToken   string
	TokenHash string
	Status    TicketStatus
}

type DeliveryEvent struct {
	ID       int64
	OrderID  string
	Attempts int
}

type Delivery struct {
	OrderID string
	Email   string
	Tickets []Ticket
}

type Notifier interface {
	Deliver(context.Context, Delivery) error
}

type Repository interface {
	ListOrderTickets(ctx context.Context, orderID, userID string) ([]Ticket, error)
	ClaimTicketDeliveries(ctx context.Context, now time.Time, limit int, lease time.Duration) ([]DeliveryEvent, error)
	LoadDelivery(ctx context.Context, event DeliveryEvent) (Delivery, error)
	CompleteTicketDelivery(ctx context.Context, eventID int64, now time.Time) error
	RetryTicketDelivery(ctx context.Context, eventID int64, now time.Time, delay time.Duration) error
}

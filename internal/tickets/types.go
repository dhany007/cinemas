package tickets

import (
	"context"
	"errors"
	"time"
)

var (
	ErrOrderNotFound     = errors.New("order not found")
	ErrTicketNotFound    = errors.New("ticket not found")
	ErrTicketAlreadyUsed = errors.New("ticket already used")
)

type TicketStatus string

const (
	TicketIssued TicketStatus = "ISSUED"
	TicketUsed   TicketStatus = "USED"
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
	ID          string
	OrderID     string
	UserID      string
	Code        string
	QRToken     string
	TokenHash   string
	Status      TicketStatus
	CheckedInAt *time.Time
}

// AdminTicket intentionally includes only customer display name, never email
// addresses, QR tokens, or token hashes.
type AdminTicket struct {
	ID                  string
	Status              TicketStatus
	CustomerDisplayName string
	MovieTitle          string
	CinemaName          string
	StudioName          string
	StartsAt            time.Time
	CheckedInAt         *time.Time
}

type ExpiringHold struct {
	OrderID   string
	ExpiresAt time.Time
	SeatCount int
}

type PaymentException struct {
	OrderID   string
	Provider  string
	Reference string
	Status    string
	PaidAt    *time.Time
}

type NotificationFailure struct {
	EventID     int64
	OrderID     string
	Attempts    int
	Status      DeliveryStatus
	AvailableAt time.Time
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
	LookupAdminTicket(ctx context.Context, qrToken string) (AdminTicket, error)
	CheckInTicket(ctx context.Context, qrToken, adminUserID string, now time.Time) (AdminTicket, error)
	ListExpiringHolds(ctx context.Context, now time.Time, limit int) ([]ExpiringHold, error)
	ListPaymentExceptions(ctx context.Context, limit int) ([]PaymentException, error)
	ListNotificationFailures(ctx context.Context, now time.Time, limit int) ([]NotificationFailure, error)
}

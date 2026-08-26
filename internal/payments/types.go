package payments

import (
	"context"
	"errors"
	"time"
)

const (
	// FakeProvider identifies the local deterministic payment provider.
	FakeProvider = "FAKE"
)

var (
	// ErrOrderNotFound indicates that the requested order does not exist.
	ErrOrderNotFound = errors.New("order not found")
	// ErrOrderNotPayable indicates that an order cannot transition to paid.
	ErrOrderNotPayable = errors.New("order is not payable")
	// ErrOrderExpired indicates that the payment arrived after its seat hold expired.
	ErrOrderExpired = errors.New("order hold expired")
)

// OrderStatus describes the payment-relevant order lifecycle.
type OrderStatus string

const (
	// OrderPendingPayment means an order may still be paid before expiry.
	OrderPendingPayment OrderStatus = "PENDING_PAYMENT"
	// OrderPaid means payment has completed and tickets were issued.
	OrderPaid OrderStatus = "PAID"
)

// PaymentStatus describes the final fake-payment state.
type PaymentStatus string

const (
	// PaymentSucceeded means the fake provider accepted the payment.
	PaymentSucceeded PaymentStatus = "SUCCEEDED"
)

// OrderItem is the priced ticket item in an order.
type OrderItem struct {
	ID          string
	SeatID      string
	PriceAmount string
	Currency    string
}

// Ticket is an issued ticket for a paid order item.
type Ticket struct {
	OrderItemID string
	Code        string
}

// Order is the payment-relevant representation of a booking order.
type Order struct {
	ID        string
	UserID    string
	Status    OrderStatus
	ExpiresAt time.Time
	Items     []OrderItem
	Tickets   []Ticket
}

// Payment is a completed fake-provider payment.
type Payment struct {
	Provider  string
	Reference string
	Status    PaymentStatus
	Amount    string
	Currency  string
	PaidAt    time.Time
}

// Repository atomically applies the fake-payment completion transition.
type Repository interface {
	CompleteFakePayment(ctx context.Context, orderID, userID string, now time.Time) (Payment, error)
}

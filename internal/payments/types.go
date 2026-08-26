package payments

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const (
	// FakeProviderName identifies the deterministic local-only payment adapter.
	FakeProviderName = "FAKE"
)

var (
	ErrOrderNotFound           = errors.New("order not found")
	ErrOrderNotPayable         = errors.New("order is not payable")
	ErrOrderExpired            = errors.New("order hold expired")
	ErrInvalidWebhookSignature = errors.New("invalid webhook signature")
	ErrWebhookExpired          = errors.New("webhook timestamp is outside replay window")
	ErrPaymentNotFound         = errors.New("payment not found")
)

type OrderStatus string

const (
	OrderPendingPayment OrderStatus = "PENDING_PAYMENT"
	OrderPaid           OrderStatus = "PAID"
	OrderExpired        OrderStatus = "EXPIRED"
	OrderCancelled      OrderStatus = "CANCELLED"
)

type PaymentStatus string

const (
	PaymentPending       PaymentStatus = "PENDING"
	PaymentSucceeded     PaymentStatus = "SUCCEEDED"
	PaymentFailed        PaymentStatus = "FAILED"
	PaymentExpired       PaymentStatus = "EXPIRED"
	PaymentRefundPending PaymentStatus = "REFUND_PENDING"
)

type WebhookEventStatus string

const (
	WebhookPaymentSucceeded WebhookEventStatus = "SUCCEEDED"
	WebhookPaymentFailed    WebhookEventStatus = "FAILED"
	WebhookPaymentExpired   WebhookEventStatus = "EXPIRED"
)

type OrderItem struct {
	ID          string
	SeatID      string
	PriceAmount string
	Currency    string
}

type Ticket struct {
	OrderItemID string
	Code        string
}

type Order struct {
	ID        string
	UserID    string
	Status    OrderStatus
	ExpiresAt time.Time
	Items     []OrderItem
	Tickets   []Ticket
}

// PaymentIntentRequest is sent to an external payment adapter. IdempotencyKey
// is stable for the order and must be forwarded to the provider unchanged.
type PaymentIntentRequest struct {
	OrderID        string
	Amount         string
	Currency       string
	IdempotencyKey string
}

type PaymentIntent struct {
	Provider  string
	Reference string
	Status    PaymentStatus
	Amount    string
	Currency  string
}

type Payment struct {
	Provider  string
	Reference string
	Status    PaymentStatus
	Amount    string
	Currency  string
	PaidAt    *time.Time
}

// WebhookRequest retains the raw signed body. The signature verifier must run
// before decoding or trusting any field in Body.
type WebhookRequest struct {
	Header http.Header
	Body   []byte
}

type WebhookEvent struct {
	Provider          string
	ProviderEventID   string
	ProviderReference string
	Status            WebhookEventStatus
	OccurredAt        time.Time
}

// Provider isolates provider-specific HTTP/API and webhook protocols.
type Provider interface {
	Name() string
	CreatePaymentIntent(context.Context, PaymentIntentRequest) (PaymentIntent, error)
	VerifyWebhook(WebhookRequest, time.Time) (WebhookEvent, error)
}

// Repository persists payment intents and atomically processes verified events.
// Provider calls deliberately happen outside repository transactions.
type Repository interface {
	PaymentIntentRequest(ctx context.Context, orderID, userID string, now time.Time) (PaymentIntentRequest, *PaymentIntent, error)
	SavePaymentIntent(ctx context.Context, intent PaymentIntent, orderID string, now time.Time) (PaymentIntent, error)
	ProcessWebhookEvent(ctx context.Context, event WebhookEvent, now time.Time) error
}

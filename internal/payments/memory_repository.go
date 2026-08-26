package payments

import (
	"context"
	"sync"
	"time"
)

// MemoryRepository is a concurrency-safe payment repository for tests.
type MemoryRepository struct {
	mu            sync.Mutex
	orders        map[string]Order
	payments      map[string]Payment
	webhookEvents map[string]struct{}
}

func NewMemoryRepository(orders []Order) *MemoryRepository {
	repository := &MemoryRepository{
		orders:        make(map[string]Order, len(orders)),
		payments:      make(map[string]Payment, len(orders)),
		webhookEvents: make(map[string]struct{}),
	}
	for _, order := range orders {
		repository.orders[order.ID] = copyOrder(order)
	}
	return repository
}

func (r *MemoryRepository) PaymentIntentRequest(
	ctx context.Context,
	orderID string,
	userID string,
	now time.Time,
) (PaymentIntentRequest, *PaymentIntent, error) {
	if err := ctx.Err(); err != nil {
		return PaymentIntentRequest{}, nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[orderID]
	if !ok || order.UserID != userID {
		return PaymentIntentRequest{}, nil, ErrOrderNotFound
	}
	if payment, ok := r.payments[orderID]; ok {
		return PaymentIntentRequest{}, &PaymentIntent{
			Provider: payment.Provider, Reference: payment.Reference, Status: payment.Status, Amount: payment.Amount, Currency: payment.Currency,
		}, nil
	}
	if order.Status != OrderPendingPayment {
		return PaymentIntentRequest{}, nil, ErrOrderNotPayable
	}
	if !order.ExpiresAt.After(now) {
		return PaymentIntentRequest{}, nil, ErrOrderExpired
	}
	amount, currency := paymentAmount(order.Items)
	return PaymentIntentRequest{OrderID: order.ID, Amount: amount, Currency: currency, IdempotencyKey: "payment-intent-" + order.ID}, nil, nil
}

func (r *MemoryRepository) SavePaymentIntent(
	ctx context.Context,
	intent PaymentIntent,
	orderID string,
	now time.Time,
) (PaymentIntent, error) {
	if err := ctx.Err(); err != nil {
		return PaymentIntent{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.payments[orderID]; ok {
		return PaymentIntent{Provider: existing.Provider, Reference: existing.Reference, Status: existing.Status, Amount: existing.Amount, Currency: existing.Currency}, nil
	}
	order, ok := r.orders[orderID]
	if !ok {
		return PaymentIntent{}, ErrOrderNotFound
	}
	if order.Status != OrderPendingPayment {
		return PaymentIntent{}, ErrOrderNotPayable
	}
	if !order.ExpiresAt.After(now) {
		return PaymentIntent{}, ErrOrderExpired
	}
	r.payments[orderID] = Payment{Provider: intent.Provider, Reference: intent.Reference, Status: intent.Status, Amount: intent.Amount, Currency: intent.Currency}
	return intent, nil
}

func (r *MemoryRepository) ProcessWebhookEvent(ctx context.Context, event WebhookEvent, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := event.Provider + "\x00" + event.ProviderEventID
	if _, duplicate := r.webhookEvents[key]; duplicate {
		return nil
	}
	for orderID, payment := range r.payments {
		if payment.Provider != event.Provider || payment.Reference != event.ProviderReference {
			continue
		}
		r.webhookEvents[key] = struct{}{}
		order := r.orders[orderID]
		switch event.Status {
		case WebhookPaymentSucceeded:
			if order.Status == OrderPaid || payment.Status == PaymentSucceeded {
				return nil
			}
			if order.Status != OrderPendingPayment || !order.ExpiresAt.After(now) {
				if order.Status == OrderPendingPayment {
					order.Status = OrderExpired
					r.orders[orderID] = copyOrder(order)
				}
				payment.Status = PaymentRefundPending
				paidAt := event.OccurredAt
				payment.PaidAt = &paidAt
				r.payments[orderID] = payment
				return nil
			}
			for _, item := range order.Items {
				order.Tickets = append(order.Tickets, Ticket{OrderItemID: item.ID, Code: "TKT-" + item.ID})
			}
			order.Status = OrderPaid
			paidAt := event.OccurredAt
			payment.Status = PaymentSucceeded
			payment.PaidAt = &paidAt
			r.orders[orderID] = copyOrder(order)
			r.payments[orderID] = payment
		case WebhookPaymentFailed:
			if payment.Status == PaymentPending {
				payment.Status = PaymentFailed
				r.payments[orderID] = payment
			}
		case WebhookPaymentExpired:
			if payment.Status == PaymentPending {
				payment.Status = PaymentExpired
				r.payments[orderID] = payment
			}
		}
		return nil
	}
	return ErrPaymentNotFound
}

func (r *MemoryRepository) Order(orderID string) (Order, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[orderID]
	return copyOrder(order), ok
}

func (r *MemoryRepository) Payment(orderID string) (Payment, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	payment, ok := r.payments[orderID]
	return payment, ok
}

func (r *MemoryRepository) WebhookEventCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.webhookEvents)
}

func paymentAmount(items []OrderItem) (string, string) {
	if len(items) == 0 {
		return "0.00", ""
	}
	return items[0].PriceAmount, items[0].Currency
}

func copyOrder(order Order) Order {
	order.Items = append([]OrderItem(nil), order.Items...)
	order.Tickets = append([]Ticket(nil), order.Tickets...)
	return order
}

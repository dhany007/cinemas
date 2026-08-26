package payments

import (
	"context"
	"sync"
	"time"
)

// MemoryRepository is a concurrency-safe fake-payment repository for tests.
type MemoryRepository struct {
	mu       sync.Mutex
	orders   map[string]Order
	payments map[string]Payment
}

// NewMemoryRepository creates a memory repository seeded with pending orders.
func NewMemoryRepository(orders []Order) *MemoryRepository {
	repository := &MemoryRepository{
		orders:   make(map[string]Order, len(orders)),
		payments: make(map[string]Payment, len(orders)),
	}
	for _, order := range orders {
		repository.orders[order.ID] = copyOrder(order)
	}
	return repository
}

// CompleteFakePayment marks the order paid and issues one ticket per item.
func (r *MemoryRepository) CompleteFakePayment(
	ctx context.Context,
	orderID string,
	userID string,
	now time.Time,
) (Payment, error) {
	if err := ctx.Err(); err != nil {
		return Payment{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	order, ok := r.orders[orderID]
	if !ok {
		return Payment{}, ErrOrderNotFound
	}
	if order.UserID != userID {
		return Payment{}, ErrOrderNotFound
	}
	if order.Status == OrderPaid {
		payment, ok := r.payments[orderID]
		if !ok {
			return Payment{}, ErrOrderNotPayable
		}
		return payment, nil
	}
	if order.Status != OrderPendingPayment {
		return Payment{}, ErrOrderNotPayable
	}
	if !order.ExpiresAt.After(now) {
		return Payment{}, ErrOrderExpired
	}

	amount, currency := paymentAmount(order.Items)
	for _, item := range order.Items {
		order.Tickets = append(order.Tickets, Ticket{
			OrderItemID: item.ID,
			Code:        "TKT-" + item.ID,
		})
	}
	order.Status = OrderPaid
	r.orders[orderID] = copyOrder(order)
	payment := Payment{
		Provider:  FakeProvider,
		Reference: "fake-" + orderID,
		Status:    PaymentSucceeded,
		Amount:    amount,
		Currency:  currency,
		PaidAt:    now,
	}
	r.payments[orderID] = payment
	return payment, nil
}

// Order returns a test order snapshot.
func (r *MemoryRepository) Order(orderID string) (Order, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	order, ok := r.orders[orderID]
	return copyOrder(order), ok
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

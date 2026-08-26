package booking

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemoryRepository is a concurrency-safe repository used by unit and API tests.
type MemoryRepository struct {
	mu                  sync.RWMutex
	seats               map[string]Seat
	orders              map[string]Order
	ordersByIdempotency map[string]Order
}

// NewMemoryRepository creates an in-memory repository with the supplied seat inventory.
func NewMemoryRepository(seats []Seat) *MemoryRepository {
	repository := &MemoryRepository{
		seats:               make(map[string]Seat, len(seats)),
		orders:              make(map[string]Order),
		ordersByIdempotency: make(map[string]Order),
	}
	for _, seat := range seats {
		repository.seats[seat.ID] = seat
	}
	return repository
}

// FindOrderByIdempotency returns the order previously created for a user and key.
func (r *MemoryRepository) FindOrderByIdempotency(_ context.Context, userID, key string) (Order, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	order, ok := r.ordersByIdempotency[idempotencyMapKey(userID, key)]
	return copyOrder(order), ok, nil
}

// CreateHold atomically stores an order and marks all its seats held.
func (r *MemoryRepository) CreateHold(ctx context.Context, order Order, now time.Time) (Order, error) {
	if err := ctx.Err(); err != nil {
		return Order{}, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := idempotencyMapKey(order.UserID, order.IdempotencyKey)
	if existing, ok := r.ordersByIdempotency[key]; ok {
		if sameHoldRequest(existing, order.ShowtimeID, orderSeatIDs(order)) {
			return copyOrder(existing), nil
		}
		return Order{}, ErrIdempotencyKeyReused
	}

	for _, item := range order.Items {
		seat, ok := r.seats[item.SeatID]
		if !ok || seat.ShowtimeID != order.ShowtimeID || !isAvailableForHold(seat, now) {
			return Order{}, ErrSeatUnavailable
		}
	}

	for _, item := range order.Items {
		seat := r.seats[item.SeatID]
		seat.Status = SeatHeld
		seat.HoldOrderID = order.ID
		seat.HoldExpiresAt = order.ExpiresAt
		r.seats[item.SeatID] = seat
	}
	r.ordersByIdempotency[key] = copyOrder(order)
	r.orders[order.ID] = copyOrder(order)
	return copyOrder(order), nil
}

func (r *MemoryRepository) FindOrder(_ context.Context, orderID, userID string) (Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	order, found := r.orders[orderID]
	if !found || order.UserID != userID {
		return Order{}, ErrOrderNotFound
	}
	return copyOrder(order), nil
}

func (r *MemoryRepository) ListOrders(_ context.Context, userID string) ([]Order, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	orders := make([]Order, 0)
	for _, order := range r.orders {
		if order.UserID == userID {
			orders = append(orders, copyOrder(order))
		}
	}
	sort.Slice(orders, func(i, j int) bool { return orders[i].CreatedAt.After(orders[j].CreatedAt) })
	return orders, nil
}

func (r *MemoryRepository) CancelOrder(ctx context.Context, orderID, userID string, now time.Time) (Order, error) {
	if err := ctx.Err(); err != nil {
		return Order{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireLocked(now, len(r.orders))
	order, found := r.orders[orderID]
	if !found || order.UserID != userID {
		return Order{}, ErrOrderNotFound
	}
	if order.Status != OrderPendingPayment {
		return Order{}, ErrOrderNotCancellable
	}
	order.Status = OrderCancelled
	r.releaseLocked(order.ID)
	r.storeLocked(order)
	return copyOrder(order), nil
}

func (r *MemoryRepository) ExpirePendingHolds(ctx context.Context, now time.Time, limit int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.expireLocked(now, limit), nil
}

// Seat returns the current state of a test seat.
func (r *MemoryRepository) Seat(id string) (Seat, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seat, ok := r.seats[id]
	return seat, ok
}

func (r *MemoryRepository) expireLocked(now time.Time, limit int) int {
	if limit <= 0 {
		return 0
	}
	orders := make([]Order, 0)
	for _, order := range r.orders {
		if order.Status == OrderPendingPayment && !order.ExpiresAt.After(now) {
			orders = append(orders, order)
		}
	}
	sort.Slice(orders, func(i, j int) bool { return orders[i].ExpiresAt.Before(orders[j].ExpiresAt) })
	if len(orders) > limit {
		orders = orders[:limit]
	}
	for _, order := range orders {
		order.Status = OrderExpired
		r.releaseLocked(order.ID)
		r.storeLocked(order)
	}
	return len(orders)
}

func (r *MemoryRepository) releaseLocked(orderID string) {
	for id, seat := range r.seats {
		if seat.Status == SeatHeld && seat.HoldOrderID == orderID {
			seat.Status, seat.HoldOrderID, seat.HoldExpiresAt = SeatAvailable, "", time.Time{}
			r.seats[id] = seat
		}
	}
}

func (r *MemoryRepository) storeLocked(order Order) {
	r.orders[order.ID] = copyOrder(order)
	r.ordersByIdempotency[idempotencyMapKey(order.UserID, order.IdempotencyKey)] = copyOrder(order)
}

func isAvailableForHold(seat Seat, now time.Time) bool {
	return seat.Status == SeatAvailable || (seat.Status == SeatHeld && !seat.HoldExpiresAt.After(now))
}

func idempotencyMapKey(userID, key string) string {
	return userID + "\x00" + key
}

func orderSeatIDs(order Order) []string {
	seatIDs := make([]string, len(order.Items))
	for i, item := range order.Items {
		seatIDs[i] = item.SeatID
	}
	return seatIDs
}

func copyOrder(order Order) Order {
	order.Items = append([]OrderItem(nil), order.Items...)
	if order.Payment != nil {
		payment := *order.Payment
		order.Payment = &payment
	}
	return order
}

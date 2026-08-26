package booking

import (
	"context"
	"sync"
	"time"
)

type MemoryRepository struct {
	mu                  sync.RWMutex
	seats               map[string]Seat
	ordersByIdempotency map[string]Order
}

func NewMemoryRepository(seats []Seat) *MemoryRepository {
	repository := &MemoryRepository{
		seats:               make(map[string]Seat, len(seats)),
		ordersByIdempotency: make(map[string]Order),
	}
	for _, seat := range seats {
		repository.seats[seat.ID] = seat
	}
	return repository
}

func (r *MemoryRepository) FindOrderByIdempotency(_ context.Context, userID, key string) (Order, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	order, ok := r.ordersByIdempotency[idempotencyMapKey(userID, key)]
	return copyOrder(order), ok, nil
}

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
	return copyOrder(order), nil
}

func (r *MemoryRepository) Seat(id string) (Seat, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seat, ok := r.seats[id]
	return seat, ok
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
	return order
}

package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// BookingRepository persists booking holds in PostgreSQL.
type BookingRepository struct {
	pool *pgxpool.Pool
}

// NewBookingRepository creates a PostgreSQL-backed booking repository.
func NewBookingRepository(pool *pgxpool.Pool) *BookingRepository {
	return &BookingRepository{pool: pool}
}

// FindOrderByIdempotency returns the existing order for a user and idempotency key.
func (r *BookingRepository) FindOrderByIdempotency(
	ctx context.Context,
	userID, key string,
) (booking.Order, bool, error) {
	order, err := findOrderByIdempotency(ctx, r.pool, userID, key)
	if errors.Is(err, pgx.ErrNoRows) {
		return booking.Order{}, false, nil
	}
	if err != nil {
		return booking.Order{}, false, fmt.Errorf("query order by idempotency key: %w", err)
	}
	return order, true, nil
}

// CreateHold atomically creates an order and marks its requested seats held.
func (r *BookingRepository) CreateHold(
	ctx context.Context,
	order booking.Order,
	nowTime time.Time,
) (booking.Order, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return booking.Order{}, fmt.Errorf("begin booking transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if existing, found, err := findExistingOrder(ctx, tx, order); err != nil {
		return booking.Order{}, err
	} else if found {
		return existing, nil
	}

	seatIDs := orderSeatIDs(order)
	rows, err := tx.Query(ctx, `
SELECT id::text, status, hold_expires_at
FROM showtime_seats
WHERE showtime_id = $1 AND id = ANY($2::uuid[])
ORDER BY id
FOR UPDATE`, order.ShowtimeID, seatIDs)
	if err != nil {
		return booking.Order{}, fmt.Errorf("lock showtime seats: %w", err)
	}

	lockedSeats := make([]lockedSeat, 0, len(seatIDs))
	for rows.Next() {
		var seat lockedSeat
		if err := rows.Scan(&seat.id, &seat.status, &seat.holdExpiresAt); err != nil {
			rows.Close()
			return booking.Order{}, fmt.Errorf("scan locked showtime seat: %w", err)
		}
		lockedSeats = append(lockedSeats, seat)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return booking.Order{}, fmt.Errorf("iterate locked showtime seats: %w", err)
	}
	rows.Close()

	// A concurrent duplicate request can have waited for the seat lock. Check
	// idempotency again after acquiring the lock before treating the seat as held.
	if existing, found, err := findExistingOrder(ctx, tx, order); err != nil {
		return booking.Order{}, err
	} else if found {
		return existing, nil
	}

	if len(lockedSeats) != len(seatIDs) || !allSeatsAvailable(lockedSeats, nowTime) {
		return booking.Order{}, booking.ErrSeatUnavailable
	}

	if _, err := tx.Exec(ctx, `
INSERT INTO orders (id, user_id, showtime_id, booking_code, idempotency_key, status, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		order.ID,
		order.UserID,
		order.ShowtimeID,
		bookingCode(order.ID),
		order.IdempotencyKey,
		order.Status,
		order.ExpiresAt,
	); err != nil {
		return booking.Order{}, fmt.Errorf("insert order: %w", err)
	}

	for _, item := range order.Items {
		if _, err := tx.Exec(ctx, `
INSERT INTO order_items (order_id, showtime_seat_id, price_amount, currency)
SELECT $1, id, price_amount, currency
FROM showtime_seats
WHERE id = $2`, order.ID, item.SeatID); err != nil {
			return booking.Order{}, fmt.Errorf("insert order item: %w", err)
		}
	}

	if _, err := tx.Exec(ctx, `
UPDATE showtime_seats
SET status = $1, hold_order_id = $2, hold_expires_at = $3, updated_at = now()
WHERE id = ANY($4::uuid[])`, booking.SeatHeld, order.ID, order.ExpiresAt, seatIDs); err != nil {
		return booking.Order{}, fmt.Errorf("mark seats held: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return booking.Order{}, fmt.Errorf("commit booking transaction: %w", err)
	}
	return order, nil
}

type queryer interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type lockedSeat struct {
	id            string
	status        booking.SeatStatus
	holdExpiresAt *time.Time
}

func findExistingOrder(ctx context.Context, queryer queryer, requested booking.Order) (booking.Order, bool, error) {
	existing, err := findOrderByIdempotency(ctx, queryer, requested.UserID, requested.IdempotencyKey)
	if errors.Is(err, pgx.ErrNoRows) {
		return booking.Order{}, false, nil
	}
	if err != nil {
		return booking.Order{}, false, fmt.Errorf("find idempotent order in transaction: %w", err)
	}
	if !sameOrderRequest(existing, requested) {
		return booking.Order{}, false, booking.ErrIdempotencyKeyReused
	}
	return existing, true, nil
}

func findOrderByIdempotency(ctx context.Context, queryer queryer, userID, key string) (booking.Order, error) {
	var order booking.Order
	if err := queryer.QueryRow(ctx, `
SELECT id::text, user_id::text, showtime_id::text, idempotency_key, status, expires_at
FROM orders
WHERE user_id = $1 AND idempotency_key = $2`, userID, key).Scan(
		&order.ID,
		&order.UserID,
		&order.ShowtimeID,
		&order.IdempotencyKey,
		&order.Status,
		&order.ExpiresAt,
	); err != nil {
		return booking.Order{}, err
	}

	rows, err := queryer.Query(ctx, `
SELECT showtime_seat_id::text
FROM order_items
WHERE order_id = $1
ORDER BY showtime_seat_id`, order.ID)
	if err != nil {
		return booking.Order{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var seatID string
		if err := rows.Scan(&seatID); err != nil {
			return booking.Order{}, err
		}
		order.Items = append(order.Items, booking.OrderItem{SeatID: seatID})
	}
	if err := rows.Err(); err != nil {
		return booking.Order{}, err
	}
	return order, nil
}

func allSeatsAvailable(seats []lockedSeat, nowTime time.Time) bool {
	for _, seat := range seats {
		if seat.status == booking.SeatAvailable {
			continue
		}
		if seat.status == booking.SeatHeld && seat.holdExpiresAt != nil && !seat.holdExpiresAt.After(nowTime) {
			continue
		}
		return false
	}
	return true
}

func orderSeatIDs(order booking.Order) []string {
	seatIDs := make([]string, len(order.Items))
	for i, item := range order.Items {
		seatIDs[i] = item.SeatID
	}
	return seatIDs
}

func sameOrderRequest(existing, requested booking.Order) bool {
	if existing.ShowtimeID != requested.ShowtimeID || len(existing.Items) != len(requested.Items) {
		return false
	}
	for i, item := range existing.Items {
		if item.SeatID != requested.Items[i].SeatID {
			return false
		}
	}
	return true
}

func bookingCode(orderID string) string {
	return "BKG-" + strings.ToUpper(strings.ReplaceAll(orderID[:8], "-", ""))
}

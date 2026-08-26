package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/citradigital/cinemas/internal/payments"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PaymentsRepository completes development fake payments in PostgreSQL.
type PaymentsRepository struct {
	pool *pgxpool.Pool
}

// NewPaymentsRepository creates a PostgreSQL-backed payments repository.
func NewPaymentsRepository(pool *pgxpool.Pool) *PaymentsRepository {
	return &PaymentsRepository{pool: pool}
}

// CompleteFakePayment atomically pays an eligible order, sells its seats, and issues tickets.
func (r *PaymentsRepository) CompleteFakePayment(
	ctx context.Context,
	orderID string,
	now time.Time,
) (payments.Payment, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return payments.Payment{}, fmt.Errorf("begin fake payment transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `
SELECT status, expires_at
FROM orders
WHERE id = $1
FOR UPDATE`, orderID).Scan(&status, &expiresAt); errors.Is(err, pgx.ErrNoRows) {
		return payments.Payment{}, payments.ErrOrderNotFound
	} else if err != nil {
		return payments.Payment{}, fmt.Errorf("lock order: %w", err)
	}

	if status == string(payments.OrderPaid) {
		payment, err := findFakePayment(ctx, tx, orderID)
		if err != nil {
			return payments.Payment{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return payments.Payment{}, fmt.Errorf("commit repeated fake payment: %w", err)
		}
		return payment, nil
	}
	if status != string(payments.OrderPendingPayment) {
		return payments.Payment{}, payments.ErrOrderNotPayable
	}
	if !expiresAt.After(now) {
		return payments.Payment{}, payments.ErrOrderExpired
	}

	var amount, currency string
	if err := tx.QueryRow(ctx, `
SELECT COALESCE(SUM(price_amount), 0)::text, MIN(currency)::text
FROM order_items
WHERE order_id = $1`, orderID).Scan(&amount, &currency); err != nil {
		return payments.Payment{}, fmt.Errorf("calculate payment amount: %w", err)
	}

	reference := "fake-" + orderID
	if _, err := tx.Exec(ctx, `
INSERT INTO payments (order_id, provider, provider_reference, status, amount, currency, paid_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (provider, provider_reference) DO NOTHING`,
		orderID,
		payments.FakeProvider,
		reference,
		payments.PaymentSucceeded,
		amount,
		currency,
		now,
	); err != nil {
		return payments.Payment{}, fmt.Errorf("insert fake payment: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE showtime_seats
SET status = 'SOLD', hold_order_id = NULL, hold_expires_at = NULL, updated_at = now()
WHERE hold_order_id = $1 AND status = 'HELD'`, orderID); err != nil {
		return payments.Payment{}, fmt.Errorf("mark seats sold: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE orders
SET status = $1, updated_at = now()
WHERE id = $2`, payments.OrderPaid, orderID); err != nil {
		return payments.Payment{}, fmt.Errorf("mark order paid: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tickets (order_id, order_item_id, ticket_code, qr_token_hash)
SELECT
    order_id,
    id,
    'TKT-' || REPLACE(id::text, '-', ''),
    ENCODE(DIGEST(id::text, 'sha256'), 'hex')
FROM order_items
WHERE order_id = $1
ON CONFLICT (order_item_id) DO NOTHING`, orderID); err != nil {
		return payments.Payment{}, fmt.Errorf("issue tickets: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return payments.Payment{}, fmt.Errorf("commit fake payment: %w", err)
	}
	return payments.Payment{
		Provider:  payments.FakeProvider,
		Reference: reference,
		Status:    payments.PaymentSucceeded,
		Amount:    amount,
		Currency:  currency,
		PaidAt:    now,
	}, nil
}

type paymentQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findFakePayment(ctx context.Context, queryer paymentQueryer, orderID string) (payments.Payment, error) {
	var payment payments.Payment
	if err := queryer.QueryRow(ctx, `
SELECT provider, provider_reference, status, amount::text, currency::text, paid_at
FROM payments
WHERE order_id = $1 AND provider = $2`, orderID, payments.FakeProvider).Scan(
		&payment.Provider,
		&payment.Reference,
		&payment.Status,
		&payment.Amount,
		&payment.Currency,
		&payment.PaidAt,
	); err != nil {
		return payments.Payment{}, fmt.Errorf("find completed fake payment: %w", err)
	}
	return payment, nil
}

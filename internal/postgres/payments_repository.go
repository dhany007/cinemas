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

// PaymentsRepository persists intents and applies verified provider events.
type PaymentsRepository struct {
	pool *pgxpool.Pool
}

func NewPaymentsRepository(pool *pgxpool.Pool) *PaymentsRepository {
	return &PaymentsRepository{pool: pool}
}

func (r *PaymentsRepository) PaymentIntentRequest(
	ctx context.Context,
	orderID string,
	userID string,
	now time.Time,
) (payments.PaymentIntentRequest, *payments.PaymentIntent, error) {
	var status string
	var expiresAt time.Time
	err := r.pool.QueryRow(ctx, `
SELECT status, expires_at
FROM orders
WHERE id = $1 AND user_id = $2`, orderID, userID).Scan(&status, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.PaymentIntentRequest{}, nil, payments.ErrOrderNotFound
	}
	if err != nil {
		return payments.PaymentIntentRequest{}, nil, fmt.Errorf("find order for payment intent: %w", err)
	}

	var intent payments.PaymentIntent
	err = r.pool.QueryRow(ctx, `
SELECT provider, provider_reference, status, amount::text, currency::text
FROM payments
WHERE order_id = $1
ORDER BY created_at DESC
LIMIT 1`, orderID).Scan(&intent.Provider, &intent.Reference, &intent.Status, &intent.Amount, &intent.Currency)
	if err == nil {
		return payments.PaymentIntentRequest{}, &intent, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return payments.PaymentIntentRequest{}, nil, fmt.Errorf("find existing payment intent: %w", err)
	}
	if status != string(payments.OrderPendingPayment) {
		return payments.PaymentIntentRequest{}, nil, payments.ErrOrderNotPayable
	}
	if !expiresAt.After(now) {
		return payments.PaymentIntentRequest{}, nil, payments.ErrOrderExpired
	}

	var amount, currency string
	if err := r.pool.QueryRow(ctx, `
SELECT COALESCE(SUM(price_amount), 0)::text, MIN(currency)::text
FROM order_items
WHERE order_id = $1`, orderID).Scan(&amount, &currency); err != nil {
		return payments.PaymentIntentRequest{}, nil, fmt.Errorf("calculate payment amount: %w", err)
	}
	return payments.PaymentIntentRequest{
		OrderID:        orderID,
		Amount:         amount,
		Currency:       currency,
		IdempotencyKey: "payment-intent-" + orderID,
	}, nil, nil
}

func (r *PaymentsRepository) SavePaymentIntent(
	ctx context.Context,
	intent payments.PaymentIntent,
	orderID string,
	now time.Time,
) (payments.PaymentIntent, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return payments.PaymentIntent{}, fmt.Errorf("begin save payment intent: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	var expiresAt time.Time
	if err := tx.QueryRow(ctx, `SELECT status, expires_at FROM orders WHERE id = $1 FOR UPDATE`, orderID).Scan(&status, &expiresAt); errors.Is(err, pgx.ErrNoRows) {
		return payments.PaymentIntent{}, payments.ErrOrderNotFound
	} else if err != nil {
		return payments.PaymentIntent{}, fmt.Errorf("lock order for payment intent: %w", err)
	}
	if existing, found, err := findPaymentIntent(ctx, tx, orderID); err != nil {
		return payments.PaymentIntent{}, err
	} else if found {
		if err := tx.Commit(ctx); err != nil {
			return payments.PaymentIntent{}, fmt.Errorf("commit existing payment intent: %w", err)
		}
		return existing, nil
	}
	if status != string(payments.OrderPendingPayment) {
		return payments.PaymentIntent{}, payments.ErrOrderNotPayable
	}
	if !expiresAt.After(now) {
		return payments.PaymentIntent{}, payments.ErrOrderExpired
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO payments (order_id, provider, provider_reference, status, amount, currency)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (provider, provider_reference) DO NOTHING`,
		orderID, intent.Provider, intent.Reference, intent.Status, intent.Amount, intent.Currency); err != nil {
		return payments.PaymentIntent{}, fmt.Errorf("insert payment intent: %w", err)
	}
	stored, found, err := findPaymentIntent(ctx, tx, orderID)
	if err != nil {
		return payments.PaymentIntent{}, err
	}
	if !found {
		return payments.PaymentIntent{}, fmt.Errorf("save payment intent: payment was not persisted")
	}
	if err := tx.Commit(ctx); err != nil {
		return payments.PaymentIntent{}, fmt.Errorf("commit payment intent: %w", err)
	}
	return stored, nil
}

// ProcessWebhookEvent atomically records a verified event, deduplicates it,
// and finalizes the corresponding order when the successful payment is timely.
func (r *PaymentsRepository) ProcessWebhookEvent(ctx context.Context, event payments.WebhookEvent, now time.Time) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin payment webhook transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var eventID string
	err = tx.QueryRow(ctx, `
INSERT INTO payment_webhook_events (provider, provider_event_id)
VALUES ($1, $2)
ON CONFLICT (provider, provider_event_id) DO NOTHING
RETURNING id::text`, event.Provider, event.ProviderEventID).Scan(&eventID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit duplicate payment webhook: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("record payment webhook event: %w", err)
	}

	var orderID, paymentStatus, orderStatus string
	var expiresAt time.Time
	err = tx.QueryRow(ctx, `
SELECT p.order_id::text, p.status, o.status, o.expires_at
FROM payments p
JOIN orders o ON o.id = p.order_id
WHERE p.provider = $1 AND p.provider_reference = $2
FOR UPDATE OF p, o`, event.Provider, event.ProviderReference).Scan(&orderID, &paymentStatus, &orderStatus, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := markWebhookProcessed(ctx, tx, eventID); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit unrecognized payment webhook: %w", err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("lock payment and order for webhook: %w", err)
	}

	switch event.Status {
	case payments.WebhookPaymentSucceeded:
		if paymentStatus != string(payments.PaymentSucceeded) && orderStatus != string(payments.OrderPaid) {
			if orderStatus != string(payments.OrderPendingPayment) || !expiresAt.After(now) {
				if orderStatus == string(payments.OrderPendingPayment) {
					if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'EXPIRED', updated_at = now() WHERE id = $1`, orderID); err != nil {
						return fmt.Errorf("mark late order expired: %w", err)
					}
					if _, err := tx.Exec(ctx, `
UPDATE showtime_seats
SET status = 'AVAILABLE', hold_order_id = NULL, hold_expires_at = NULL, updated_at = now()
WHERE hold_order_id = $1 AND status = 'HELD'`, orderID); err != nil {
						return fmt.Errorf("release late payment seats: %w", err)
					}
				}
				if _, err := tx.Exec(ctx, `
UPDATE payments SET status = 'REFUND_PENDING', paid_at = $1, updated_at = now()
WHERE order_id = $2`, event.OccurredAt, orderID); err != nil {
					return fmt.Errorf("mark late payment for refund: %w", err)
				}
				if err := recordPaymentAuditEvent(ctx, tx, orderID, "PAYMENT_REFUND_PENDING"); err != nil {
					return err
				}
			} else if err := finalizePaidOrder(ctx, tx, orderID, event.OccurredAt); err != nil {
				return err
			}
		}
	case payments.WebhookPaymentFailed:
		if paymentStatus == string(payments.PaymentPending) {
			if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'FAILED', updated_at = now() WHERE order_id = $1`, orderID); err != nil {
				return fmt.Errorf("mark failed payment: %w", err)
			}
		}
	case payments.WebhookPaymentExpired:
		if paymentStatus == string(payments.PaymentPending) {
			if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'EXPIRED', updated_at = now() WHERE order_id = $1`, orderID); err != nil {
				return fmt.Errorf("mark expired payment: %w", err)
			}
		}
	}
	if err := markWebhookProcessed(ctx, tx, eventID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit payment webhook: %w", err)
	}
	return nil
}

type paymentQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findPaymentIntent(ctx context.Context, queryer paymentQueryer, orderID string) (payments.PaymentIntent, bool, error) {
	var intent payments.PaymentIntent
	err := queryer.QueryRow(ctx, `
SELECT provider, provider_reference, status, amount::text, currency::text
FROM payments
WHERE order_id = $1
ORDER BY created_at DESC
LIMIT 1`, orderID).Scan(&intent.Provider, &intent.Reference, &intent.Status, &intent.Amount, &intent.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return payments.PaymentIntent{}, false, nil
	}
	if err != nil {
		return payments.PaymentIntent{}, false, fmt.Errorf("find payment intent: %w", err)
	}
	return intent, true, nil
}

func finalizePaidOrder(ctx context.Context, tx pgx.Tx, orderID string, paidAt time.Time) error {
	if _, err := tx.Exec(ctx, `UPDATE payments SET status = 'SUCCEEDED', paid_at = $1, updated_at = now() WHERE order_id = $2`, paidAt, orderID); err != nil {
		return fmt.Errorf("mark payment succeeded: %w", err)
	}
	if _, err := tx.Exec(ctx, `
UPDATE showtime_seats
SET status = 'SOLD', hold_order_id = NULL, hold_expires_at = NULL, updated_at = now()
WHERE hold_order_id = $1 AND status = 'HELD'`, orderID); err != nil {
		return fmt.Errorf("mark seats sold: %w", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE orders SET status = 'PAID', updated_at = now() WHERE id = $1`, orderID); err != nil {
		return fmt.Errorf("mark order paid: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO tickets (order_id, order_item_id, ticket_code, qr_token_hash)
SELECT generated.order_id, generated.id, generated.ticket_code, ENCODE(DIGEST(generated.ticket_code, 'sha256'), 'hex')
FROM (
    SELECT order_id, id, 'TKT-' || ENCODE(GEN_RANDOM_BYTES(32), 'hex') AS ticket_code
    FROM order_items
    WHERE order_id = $1
) AS generated
ON CONFLICT (order_item_id) DO NOTHING`, orderID); err != nil {
		return fmt.Errorf("issue tickets: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO outbox_events (aggregate_type, aggregate_id, event_type, payload)
VALUES ('ORDER', $1, 'TICKET_DELIVERY_REQUESTED', JSONB_BUILD_OBJECT('order_id', $1::uuid))`, orderID); err != nil {
		return fmt.Errorf("enqueue ticket delivery: %w", err)
	}
	return recordPaymentAuditEvent(ctx, tx, orderID, "PAYMENT_SUCCEEDED")
}

func recordPaymentAuditEvent(ctx context.Context, tx pgx.Tx, orderID string, action string) error {
	if _, err := tx.Exec(ctx, `INSERT INTO audit_events (entity_type, entity_id, action) VALUES ('ORDER', $1, $2)`, orderID, action); err != nil {
		return fmt.Errorf("record payment audit event: %w", err)
	}
	return nil
}

func markWebhookProcessed(ctx context.Context, tx pgx.Tx, eventID string) error {
	if _, err := tx.Exec(ctx, `UPDATE payment_webhook_events SET processed_at = now() WHERE id = $1`, eventID); err != nil {
		return fmt.Errorf("mark payment webhook processed: %w", err)
	}
	return nil
}

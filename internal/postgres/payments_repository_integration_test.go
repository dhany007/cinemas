package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPaymentsRepositoryPostgreSQLWebhookFinalization(t *testing.T) {
	databaseURL := os.Getenv("CINEMAS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CINEMAS_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	bookingRepository := NewBookingRepository(pool)
	paymentRepository := NewPaymentsRepository(pool)
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)

	t.Run("successful event finalizes once with tickets and audit event", func(t *testing.T) {
		userID, showtimeID, seatIDs := seedBookingTestData(t, pool, 1)
		order := booking.Order{ID: databaseUUID(t, pool), UserID: userID, ShowtimeID: showtimeID, IdempotencyKey: "payment-success", Status: booking.OrderPendingPayment, ExpiresAt: now.Add(time.Minute), Items: []booking.OrderItem{{SeatID: seatIDs[0]}}}
		if _, err := bookingRepository.CreateHold(context.Background(), order, now); err != nil {
			t.Fatalf("CreateHold() error = %v", err)
		}
		intent := createPostgresPaymentIntent(t, paymentRepository, order.ID, userID, now)
		event := payments.WebhookEvent{Provider: payments.FakeProviderName, ProviderEventID: "evt-" + order.ID, ProviderReference: intent.Reference, Status: payments.WebhookPaymentSucceeded, OccurredAt: now}
		if err := paymentRepository.ProcessWebhookEvent(context.Background(), event, now); err != nil {
			t.Fatalf("ProcessWebhookEvent() error = %v", err)
		}
		if err := paymentRepository.ProcessWebhookEvent(context.Background(), event, now); err != nil {
			t.Fatalf("ProcessWebhookEvent() duplicate error = %v", err)
		}

		assertPaymentFinalization(t, pool, order.ID, seatIDs[0], "PAID", "SUCCEEDED", "SOLD", "PAYMENT_SUCCEEDED")
		var eventCount, ticketCount, auditCount int
		if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM payment_webhook_events WHERE provider = $1 AND provider_event_id = $2`, event.Provider, event.ProviderEventID).Scan(&eventCount); err != nil {
			t.Fatalf("count webhook events: %v", err)
		}
		if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM tickets WHERE order_id = $1`, order.ID).Scan(&ticketCount); err != nil {
			t.Fatalf("count tickets: %v", err)
		}
		if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_events WHERE entity_id = $1 AND action = 'PAYMENT_SUCCEEDED'`, order.ID).Scan(&auditCount); err != nil {
			t.Fatalf("count audit events: %v", err)
		}
		if eventCount != 1 || ticketCount != 1 || auditCount != 1 {
			t.Fatalf("event/ticket/audit counts = %d/%d/%d, want 1/1/1", eventCount, ticketCount, auditCount)
		}
	})

	t.Run("late success is retained for refund without reclaiming inventory", func(t *testing.T) {
		userID, showtimeID, seatIDs := seedBookingTestData(t, pool, 1)
		order := booking.Order{ID: databaseUUID(t, pool), UserID: userID, ShowtimeID: showtimeID, IdempotencyKey: "payment-late", Status: booking.OrderPendingPayment, ExpiresAt: now.Add(time.Minute), Items: []booking.OrderItem{{SeatID: seatIDs[0]}}}
		if _, err := bookingRepository.CreateHold(context.Background(), order, now); err != nil {
			t.Fatalf("CreateHold() error = %v", err)
		}
		intent := createPostgresPaymentIntent(t, paymentRepository, order.ID, userID, now)
		event := payments.WebhookEvent{Provider: payments.FakeProviderName, ProviderEventID: "evt-late-" + order.ID, ProviderReference: intent.Reference, Status: payments.WebhookPaymentSucceeded, OccurredAt: now.Add(2 * time.Minute)}
		if err := paymentRepository.ProcessWebhookEvent(context.Background(), event, now.Add(2*time.Minute)); err != nil {
			t.Fatalf("ProcessWebhookEvent() late error = %v", err)
		}

		assertPaymentFinalization(t, pool, order.ID, seatIDs[0], "EXPIRED", "REFUND_PENDING", "AVAILABLE", "PAYMENT_REFUND_PENDING")
	})
}

func createPostgresPaymentIntent(t *testing.T, repository *PaymentsRepository, orderID, userID string, now time.Time) payments.PaymentIntent {
	t.Helper()
	request, existing, err := repository.PaymentIntentRequest(context.Background(), orderID, userID, now)
	if err != nil || existing != nil {
		t.Fatalf("PaymentIntentRequest() request=%#v existing=%#v error=%v", request, existing, err)
	}
	intent, err := repository.SavePaymentIntent(context.Background(), payments.PaymentIntent{Provider: payments.FakeProviderName, Reference: "fake-" + orderID, Status: payments.PaymentPending, Amount: request.Amount, Currency: request.Currency}, orderID, now)
	if err != nil {
		t.Fatalf("SavePaymentIntent() error = %v", err)
	}
	return intent
}

func assertPaymentFinalization(t *testing.T, pool *pgxpool.Pool, orderID, seatID, orderStatus, paymentStatus, seatStatus, auditAction string) {
	t.Helper()
	ctx := context.Background()
	var actualOrderStatus, actualPaymentStatus, actualSeatStatus, actualAuditAction string
	if err := pool.QueryRow(ctx, `SELECT status FROM orders WHERE id = $1`, orderID).Scan(&actualOrderStatus); err != nil {
		t.Fatalf("query order status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM payments WHERE order_id = $1`, orderID).Scan(&actualPaymentStatus); err != nil {
		t.Fatalf("query payment status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM showtime_seats WHERE id = $1`, seatID).Scan(&actualSeatStatus); err != nil {
		t.Fatalf("query seat status: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT action FROM audit_events WHERE entity_id = $1 ORDER BY created_at DESC, id DESC LIMIT 1`, orderID).Scan(&actualAuditAction); err != nil {
		t.Fatalf("query payment audit event: %v", err)
	}
	if actualOrderStatus != orderStatus || actualPaymentStatus != paymentStatus || actualSeatStatus != seatStatus || actualAuditAction != auditAction {
		t.Fatalf("order/payment/seat/audit = %q/%q/%q/%q, want %q/%q/%q/%q", actualOrderStatus, actualPaymentStatus, actualSeatStatus, actualAuditAction, orderStatus, paymentStatus, seatStatus, auditAction)
	}
}

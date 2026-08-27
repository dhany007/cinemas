package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/citradigital/cinemas/internal/tickets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTicketsRepositoryPostgreSQLRetrievalAndOutbox(t *testing.T) {
	databaseURL := os.Getenv("CINEMAS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CINEMAS_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	now := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	userID, showtimeID, seatIDs := seedBookingTestData(t, pool, 1)
	order := booking.Order{ID: databaseUUID(t, pool), UserID: userID, ShowtimeID: showtimeID, IdempotencyKey: "ticket-retrieval", Status: booking.OrderPendingPayment, ExpiresAt: now.Add(time.Minute), Items: []booking.OrderItem{{SeatID: seatIDs[0]}}}
	bookingRepository := NewBookingRepository(pool)
	if _, err := bookingRepository.CreateHold(context.Background(), order, now); err != nil {
		t.Fatalf("CreateHold() error = %v", err)
	}
	paymentRepository := NewPaymentsRepository(pool)
	intent := createPostgresPaymentIntent(t, paymentRepository, order.ID, userID, now)
	event := payments.WebhookEvent{Provider: payments.FakeProviderName, ProviderEventID: "evt-ticket-" + order.ID, ProviderReference: intent.Reference, Status: payments.WebhookPaymentSucceeded, OccurredAt: now}
	if err := paymentRepository.ProcessWebhookEvent(context.Background(), event, now); err != nil {
		t.Fatalf("ProcessWebhookEvent() error = %v", err)
	}
	if err := paymentRepository.ProcessWebhookEvent(context.Background(), event, now); err != nil {
		t.Fatalf("ProcessWebhookEvent() duplicate error = %v", err)
	}

	repository := NewTicketsRepository(pool)
	ownerTickets, err := repository.ListOrderTickets(context.Background(), order.ID, userID)
	if err != nil {
		t.Fatalf("ListOrderTickets() error = %v", err)
	}
	if len(ownerTickets) != 1 || len(ownerTickets[0].QRToken) < 64 || ownerTickets[0].TokenHash != "" {
		t.Fatalf("owner tickets = %#v, want opaque QR token without hash", ownerTickets)
	}
	if _, err := repository.ListOrderTickets(context.Background(), order.ID, databaseUUID(t, pool)); !errors.Is(err, tickets.ErrOrderNotFound) {
		t.Fatalf("foreign ListOrderTickets() error = %v, want ErrOrderNotFound", err)
	}

	events, err := repository.ClaimTicketDeliveries(context.Background(), now, 10, time.Minute)
	if err != nil {
		t.Fatalf("ClaimTicketDeliveries() error = %v", err)
	}
	if len(events) != 1 || events[0].OrderID != order.ID {
		t.Fatalf("events = %#v, want one order delivery event", events)
	}
	delivery, err := repository.LoadDelivery(context.Background(), events[0])
	if err != nil || len(delivery.Tickets) != 1 || delivery.Tickets[0].TokenHash != "" {
		t.Fatalf("LoadDelivery() delivery=%#v error=%v", delivery, err)
	}
	if err := repository.CompleteTicketDelivery(context.Background(), events[0].ID, now); err != nil {
		t.Fatalf("CompleteTicketDelivery() error = %v", err)
	}
	var status string
	if err := pool.QueryRow(context.Background(), `SELECT status FROM outbox_events WHERE id = $1`, events[0].ID).Scan(&status); err != nil {
		t.Fatalf("query delivery status: %v", err)
	}
	if status != "COMPLETED" {
		t.Fatalf("outbox status = %q, want COMPLETED", status)
	}
	var outboxCount int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM outbox_events WHERE aggregate_id = $1 AND event_type = 'TICKET_DELIVERY_REQUESTED'`, order.ID).Scan(&outboxCount); err != nil {
		t.Fatalf("count ticket delivery events: %v", err)
	}
	if outboxCount != 1 {
		t.Fatalf("ticket delivery event count = %d, want 1", outboxCount)
	}
}

package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/citradigital/cinemas/internal/tickets"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestTicketsRepositoryPostgreSQLCheckInIsAtomic(t *testing.T) {
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
	order := booking.Order{ID: databaseUUID(t, pool), UserID: userID, ShowtimeID: showtimeID, IdempotencyKey: "ticket-check-in", Status: booking.OrderPendingPayment, ExpiresAt: now.Add(time.Minute), Items: []booking.OrderItem{{SeatID: seatIDs[0]}}}
	if _, err := NewBookingRepository(pool).CreateHold(context.Background(), order, now); err != nil {
		t.Fatalf("CreateHold() error = %v", err)
	}
	paymentRepository := NewPaymentsRepository(pool)
	intent := createPostgresPaymentIntent(t, paymentRepository, order.ID, userID, now)
	if err := paymentRepository.ProcessWebhookEvent(context.Background(), payments.WebhookEvent{Provider: payments.FakeProviderName, ProviderEventID: "evt-check-in-" + order.ID, ProviderReference: intent.Reference, Status: payments.WebhookPaymentSucceeded, OccurredAt: now}, now); err != nil {
		t.Fatalf("ProcessWebhookEvent() error = %v", err)
	}
	repository := NewTicketsRepository(pool)
	ownerTickets, err := repository.ListOrderTickets(context.Background(), order.ID, userID)
	if err != nil || len(ownerTickets) != 1 {
		t.Fatalf("ListOrderTickets() tickets=%#v error=%v", ownerTickets, err)
	}
	adminIDs := []string{databaseUUID(t, pool), databaseUUID(t, pool)}
	for _, adminID := range adminIDs {
		if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, email, display_name) VALUES ($1, $2, 'Admin')`, adminID, adminID+"@example.test"); err != nil {
			t.Fatalf("insert admin: %v", err)
		}
	}

	var successes, conflicts int
	var mutex sync.Mutex
	var wait sync.WaitGroup
	for _, adminID := range adminIDs {
		adminID := adminID
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, err := repository.CheckInTicket(context.Background(), ownerTickets[0].QRToken, adminID, now)
			if err == nil {
				mutex.Lock()
				successes++
				mutex.Unlock()
				return
			}
			if errors.Is(err, tickets.ErrTicketAlreadyUsed) {
				mutex.Lock()
				conflicts++
				mutex.Unlock()
				return
			}
			t.Errorf("CheckInTicket() error = %v", err)
		}()
	}
	wait.Wait()
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes/conflicts = %d/%d, want 1/1", successes, conflicts)
	}
	stored, err := repository.LookupAdminTicket(context.Background(), ownerTickets[0].QRToken)
	if err != nil || stored.Status != tickets.TicketUsed || stored.CheckedInAt == nil {
		t.Fatalf("LookupAdminTicket() ticket=%#v error=%v", stored, err)
	}
	var auditCount int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM audit_events WHERE entity_type = 'TICKET' AND entity_id = $1 AND action = 'TICKET_CHECKED_IN'`, stored.ID).Scan(&auditCount); err != nil {
		t.Fatalf("count ticket audit events: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("check-in audit events = %d, want 1", auditCount)
	}
}

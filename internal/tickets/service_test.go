package tickets

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceListsOnlyPaidOwnersTicketsWithoutHashes(t *testing.T) {
	now := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Ticket{{
		ID: "ticket-1", OrderID: "order-1", UserID: "owner-1", Code: "TKT-opaque", QRToken: "TKT-opaque", Status: TicketIssued,
	}})
	service := NewService(repository, nil, func() time.Time { return now }, time.Minute)

	tickets, err := service.ListOrderTickets(context.Background(), "order-1", "owner-1")
	if err != nil {
		t.Fatalf("ListOrderTickets() error = %v", err)
	}
	if len(tickets) != 1 || tickets[0].QRToken != "TKT-opaque" || tickets[0].TokenHash != "" {
		t.Fatalf("tickets = %#v, want opaque QR token without hash", tickets)
	}
	if _, err := service.ListOrderTickets(context.Background(), "order-1", "other-user"); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("foreign ListOrderTickets() error = %v, want ErrOrderNotFound", err)
	}
}

func TestServiceRetriesTicketDeliveryAndReconcilesLeaseExpiry(t *testing.T) {
	now := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Ticket{{
		ID: "ticket-1", OrderID: "order-1", UserID: "owner-1", Code: "TKT-opaque", QRToken: "TKT-opaque", Status: TicketIssued,
	}})
	repository.EnqueueDelivery("order-1")
	notifier := &MemoryNotifier{failuresRemaining: 1}
	service := NewService(repository, notifier, func() time.Time { return now }, time.Minute)

	processed, err := service.DeliverPending(context.Background(), 10)
	if err != nil || processed != 0 {
		t.Fatalf("first DeliverPending() = %d, %v; want retry scheduled", processed, err)
	}
	now = now.Add(time.Minute)
	processed, err = service.DeliverPending(context.Background(), 10)
	if err != nil || processed != 1 {
		t.Fatalf("retry DeliverPending() = %d, %v; want one completed delivery", processed, err)
	}
	if notifier.deliveries != 2 {
		t.Fatalf("deliveries = %d, want 2", notifier.deliveries)
	}
	if state := repository.DeliveryState("order-1"); state != DeliveryCompleted {
		t.Fatalf("delivery state = %q, want %q", state, DeliveryCompleted)
	}

	repository.EnqueueDelivery("order-1")
	if _, err := repository.ClaimTicketDeliveries(context.Background(), now, 1, time.Minute); err != nil {
		t.Fatalf("ClaimTicketDeliveries() abandoned lease error = %v", err)
	}
	now = now.Add(time.Minute)
	processed, err = service.DeliverPending(context.Background(), 10)
	if err != nil || processed != 1 {
		t.Fatalf("reconciliation DeliverPending() = %d, %v; want abandoned lease completed", processed, err)
	}
}

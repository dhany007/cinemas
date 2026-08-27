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

func TestServiceChecksInTicketOnceAndPreservesFirstCheckIn(t *testing.T) {
	now := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Ticket{{
		ID: "ticket-1", OrderID: "order-1", UserID: "owner-1", Code: "TKT-opaque", QRToken: "TKT-opaque", Status: TicketIssued,
	}})
	service := NewService(repository, nil, func() time.Time { return now }, time.Minute)

	checkedIn, err := service.CheckInTicket(context.Background(), "TKT-opaque", "admin-1")
	if err != nil || checkedIn.Status != TicketUsed || checkedIn.CheckedInAt == nil {
		t.Fatalf("CheckInTicket() ticket=%#v error=%v", checkedIn, err)
	}
	firstCheckedInAt := *checkedIn.CheckedInAt
	now = now.Add(time.Minute)
	_, err = service.CheckInTicket(context.Background(), "TKT-opaque", "admin-2")
	if !errors.Is(err, ErrTicketAlreadyUsed) {
		t.Fatalf("repeated CheckInTicket() error = %v, want ErrTicketAlreadyUsed", err)
	}
	stored, err := service.LookupAdminTicket(context.Background(), "TKT-opaque")
	if err != nil || stored.CheckedInAt == nil || !stored.CheckedInAt.Equal(firstCheckedInAt) {
		t.Fatalf("ticket after repeated scan = %#v error=%v, want original check-in timestamp", stored, err)
	}
}

func TestServiceListsOperationalExceptions(t *testing.T) {
	now := time.Date(2026, time.August, 27, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository(nil)
	repository.AddExpiringHold(ExpiringHold{OrderID: "order-hold", ExpiresAt: now.Add(5 * time.Minute), SeatCount: 2})
	repository.AddPaymentException(PaymentException{OrderID: "order-refund", Status: "REFUND_PENDING"})
	repository.AddNotificationFailure(NotificationFailure{EventID: 1, OrderID: "order-delivery", Attempts: 2, Status: DeliveryPending})
	service := NewService(repository, nil, func() time.Time { return now }, time.Minute)

	holds, err := service.ListExpiringHolds(context.Background(), 10)
	if err != nil || len(holds) != 1 || holds[0].OrderID != "order-hold" {
		t.Fatalf("ListExpiringHolds() holds=%#v error=%v", holds, err)
	}
	exceptions, err := service.ListPaymentExceptions(context.Background(), 10)
	if err != nil || len(exceptions) != 1 || exceptions[0].Status != "REFUND_PENDING" {
		t.Fatalf("ListPaymentExceptions() exceptions=%#v error=%v", exceptions, err)
	}
	failures, err := service.ListNotificationFailures(context.Background(), 10)
	if err != nil || len(failures) != 1 || failures[0].Attempts != 2 {
		t.Fatalf("ListNotificationFailures() failures=%#v error=%v", failures, err)
	}
}

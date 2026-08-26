package booking

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceCreateHold(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Seat{
		{ID: "seat-b", ShowtimeID: "showtime-1", Status: SeatAvailable},
		{ID: "seat-a", ShowtimeID: "showtime-1", Status: SeatAvailable},
	})
	service := NewService(repository, 10*time.Minute, func() time.Time { return now })

	order, err := service.CreateHold(context.Background(), CreateHoldInput{
		UserID:         "user-1",
		ShowtimeID:     "showtime-1",
		SeatIDs:        []string{"seat-b", "seat-a"},
		IdempotencyKey: "checkout-1",
	})
	if err != nil {
		t.Fatalf("CreateHold() error = %v", err)
	}

	if order.Status != OrderPendingPayment {
		t.Fatalf("order status = %q, want %q", order.Status, OrderPendingPayment)
	}
	if !order.ExpiresAt.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("order expiry = %s, want %s", order.ExpiresAt, now.Add(10*time.Minute))
	}
	if len(order.Items) != 2 || order.Items[0].SeatID != "seat-a" || order.Items[1].SeatID != "seat-b" {
		t.Fatalf("order items = %#v, want sorted selected seats", order.Items)
	}

	for _, seatID := range []string{"seat-a", "seat-b"} {
		seat, ok := repository.Seat(seatID)
		if !ok {
			t.Fatalf("seat %q not found", seatID)
		}
		if seat.Status != SeatHeld || seat.HoldOrderID != order.ID || !seat.HoldExpiresAt.Equal(order.ExpiresAt) {
			t.Fatalf("seat %q = %#v, want hold for order", seatID, seat)
		}
	}
}

func TestServiceCreateHoldRejectsUnavailableSeatWithoutPartialHold(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Seat{
		{ID: "seat-a", ShowtimeID: "showtime-1", Status: SeatAvailable},
		{ID: "seat-b", ShowtimeID: "showtime-1", Status: SeatSold},
	})
	service := NewService(repository, 10*time.Minute, func() time.Time { return now })

	_, err := service.CreateHold(context.Background(), CreateHoldInput{
		UserID:         "user-1",
		ShowtimeID:     "showtime-1",
		SeatIDs:        []string{"seat-a", "seat-b"},
		IdempotencyKey: "checkout-1",
	})
	if !errors.Is(err, ErrSeatUnavailable) {
		t.Fatalf("CreateHold() error = %v, want ErrSeatUnavailable", err)
	}

	seat, _ := repository.Seat("seat-a")
	if seat.Status != SeatAvailable {
		t.Fatalf("seat-a status = %q, want %q", seat.Status, SeatAvailable)
	}
}

func TestServiceCreateHoldReturnsExistingOrderForSameIdempotencyKey(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Seat{{ID: "seat-a", ShowtimeID: "showtime-1", Status: SeatAvailable}})
	service := NewService(repository, 10*time.Minute, func() time.Time { return now })
	input := CreateHoldInput{
		UserID:         "user-1",
		ShowtimeID:     "showtime-1",
		SeatIDs:        []string{"seat-a"},
		IdempotencyKey: "checkout-1",
	}

	first, err := service.CreateHold(context.Background(), input)
	if err != nil {
		t.Fatalf("first CreateHold() error = %v", err)
	}
	second, err := service.CreateHold(context.Background(), input)
	if err != nil {
		t.Fatalf("second CreateHold() error = %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("second order ID = %q, want %q", second.ID, first.ID)
	}
}

func TestServiceCreateHoldRejectsInvalidInput(t *testing.T) {
	service := NewService(NewMemoryRepository(nil), 10*time.Minute, time.Now)

	_, err := service.CreateHold(context.Background(), CreateHoldInput{})
	if !errors.Is(err, ErrInvalidHoldInput) {
		t.Fatalf("CreateHold() error = %v, want ErrInvalidHoldInput", err)
	}
}

func TestServiceReturnsOnlyOwnedOrderHistoryAndExpiresHolds(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Seat{{ID: "seat-a", ShowtimeID: "showtime-1", Status: SeatAvailable}})
	service := NewService(repository, 10*time.Minute, func() time.Time { return now })

	created, err := service.CreateHold(context.Background(), CreateHoldInput{
		UserID:         "user-1",
		ShowtimeID:     "showtime-1",
		SeatIDs:        []string{"seat-a"},
		IdempotencyKey: "checkout-1",
	})
	if err != nil {
		t.Fatalf("CreateHold() error = %v", err)
	}
	order, err := service.GetOrder(context.Background(), created.ID, "user-1")
	if err != nil {
		t.Fatalf("GetOrder() error = %v", err)
	}
	if order.ID != created.ID || order.Status != OrderPendingPayment || len(order.Items) != 1 {
		t.Fatalf("GetOrder() = %#v, want created pending order", order)
	}
	if _, err := service.GetOrder(context.Background(), created.ID, "another-user"); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("GetOrder() foreign user error = %v, want ErrOrderNotFound", err)
	}
	history, err := service.ListOrders(context.Background(), "user-1")
	if err != nil {
		t.Fatalf("ListOrders() error = %v", err)
	}
	if len(history) != 1 || history[0].ID != created.ID {
		t.Fatalf("ListOrders() = %#v, want one owned order", history)
	}

	now = created.ExpiresAt
	expired, err := service.ExpirePendingHolds(context.Background(), 100)
	if err != nil {
		t.Fatalf("ExpirePendingHolds() error = %v", err)
	}
	if expired != 1 {
		t.Fatalf("ExpirePendingHolds() = %d, want 1", expired)
	}
	order, err = service.GetOrder(context.Background(), created.ID, "user-1")
	if err != nil {
		t.Fatalf("GetOrder() expired order error = %v", err)
	}
	if order.Status != OrderExpired {
		t.Fatalf("expired order status = %q, want %q", order.Status, OrderExpired)
	}
	seat, found := repository.Seat("seat-a")
	if !found || seat.Status != SeatAvailable || seat.HoldOrderID != "" {
		t.Fatalf("expired seat = %#v, want released seat", seat)
	}
}

func TestServiceCancelsUnpaidHoldBeforeExpiry(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Seat{{ID: "seat-a", ShowtimeID: "showtime-1", Status: SeatAvailable}})
	service := NewService(repository, 10*time.Minute, func() time.Time { return now })
	created, err := service.CreateHold(context.Background(), CreateHoldInput{
		UserID:         "user-1",
		ShowtimeID:     "showtime-1",
		SeatIDs:        []string{"seat-a"},
		IdempotencyKey: "checkout-1",
	})
	if err != nil {
		t.Fatalf("CreateHold() error = %v", err)
	}
	cancelled, err := service.CancelOrder(context.Background(), created.ID, "user-1")
	if err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	if cancelled.Status != OrderCancelled {
		t.Fatalf("cancelled order status = %q, want %q", cancelled.Status, OrderCancelled)
	}
	seat, found := repository.Seat("seat-a")
	if !found || seat.Status != SeatAvailable || seat.HoldOrderID != "" {
		t.Fatalf("cancelled seat = %#v, want released seat", seat)
	}
	if _, err := service.CancelOrder(context.Background(), created.ID, "another-user"); !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("CancelOrder() foreign user error = %v, want ErrOrderNotFound", err)
	}
}

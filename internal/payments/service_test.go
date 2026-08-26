package payments

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceCreateFakePaymentSucceedsAndIssuesTickets(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Order{{
		ID:        "10000000-0000-4000-8000-000000000001",
		Status:    OrderPendingPayment,
		ExpiresAt: now.Add(time.Minute),
		Items: []OrderItem{{
			ID:          "20000000-0000-4000-8000-000000000001",
			SeatID:      "30000000-0000-4000-8000-000000000001",
			PriceAmount: "50000.00",
			Currency:    "IDR",
		}},
	}})
	service := NewService(repository, func() time.Time { return now })

	payment, err := service.CreateFakePayment(context.Background(), "10000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("CreateFakePayment() error = %v", err)
	}
	if payment.Status != PaymentSucceeded || payment.Provider != FakeProvider {
		t.Fatalf("payment = %#v, want succeeded fake payment", payment)
	}
	repeatedPayment, err := service.CreateFakePayment(context.Background(), "10000000-0000-4000-8000-000000000001")
	if err != nil {
		t.Fatalf("CreateFakePayment() repeat error = %v", err)
	}
	if repeatedPayment.Reference != payment.Reference {
		t.Fatalf("repeated payment = %#v, want original payment", repeatedPayment)
	}

	order, ok := repository.Order("10000000-0000-4000-8000-000000000001")
	if !ok || order.Status != OrderPaid || len(order.Tickets) != 1 {
		t.Fatalf("order = %#v, want paid order with ticket", order)
	}
}

func TestServiceCreateFakePaymentRejectsMissingOrder(t *testing.T) {
	service := NewService(NewMemoryRepository(nil), time.Now)

	_, err := service.CreateFakePayment(context.Background(), "10000000-0000-4000-8000-000000000002")
	if !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("CreateFakePayment() error = %v, want ErrOrderNotFound", err)
	}
}

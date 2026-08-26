package payments

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

const testWebhookSecret = "test-webhook-secret-must-be-at-least-32-bytes"

func TestServiceCreatesPendingIntentAndWebhookFinalizesOnlyOnce(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Order{testPendingOrder(now.Add(time.Minute))})
	provider := NewFakeProvider(testWebhookSecret, 5*time.Minute)
	service := NewService(repository, provider, func() time.Time { return now })

	intent, err := service.CreatePaymentIntent(context.Background(), testOrderID, testUserID)
	if err != nil {
		t.Fatalf("CreatePaymentIntent() error = %v", err)
	}
	if intent.Status != PaymentPending || intent.Provider != FakeProviderName {
		t.Fatalf("intent = %#v, want pending fake intent", intent)
	}
	order, ok := repository.Order(testOrderID)
	if !ok || order.Status != OrderPendingPayment || len(order.Tickets) != 0 {
		t.Fatalf("order after intent = %#v, want unchanged pending order", order)
	}

	payload := provider.SuccessPayload("evt-payment-1", intent.Reference, now)
	request := signedWebhookRequest(t, provider, payload, now)
	if err := service.ProcessWebhook(context.Background(), request); err != nil {
		t.Fatalf("ProcessWebhook() error = %v", err)
	}
	if err := service.ProcessWebhook(context.Background(), request); err != nil {
		t.Fatalf("ProcessWebhook() duplicate error = %v", err)
	}

	order, ok = repository.Order(testOrderID)
	if !ok || order.Status != OrderPaid || len(order.Tickets) != 1 {
		t.Fatalf("order after webhook = %#v, want paid order with one ticket", order)
	}
	payment, ok := repository.Payment(testOrderID)
	if !ok || payment.Status != PaymentSucceeded {
		t.Fatalf("payment = %#v, want succeeded", payment)
	}
	if count := repository.WebhookEventCount(); count != 1 {
		t.Fatalf("webhook event count = %d, want 1", count)
	}
}

func TestServiceRejectsInvalidAndStaleWebhooks(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	provider := NewFakeProvider(testWebhookSecret, 5*time.Minute)
	service := NewService(NewMemoryRepository(nil), provider, func() time.Time { return now })

	payload := provider.SuccessPayload("evt-payment-2", "fake-reference", now)
	invalid := WebhookRequest{Body: payload, Header: http.Header{"X-Payment-Timestamp": []string{now.Format(time.RFC3339)}}}
	if err := service.ProcessWebhook(context.Background(), invalid); !errors.Is(err, ErrInvalidWebhookSignature) {
		t.Fatalf("ProcessWebhook() invalid signature error = %v, want ErrInvalidWebhookSignature", err)
	}
	staleAt := now.Add(-6 * time.Minute)
	stale := signedWebhookRequest(t, provider, payload, staleAt)
	if err := service.ProcessWebhook(context.Background(), stale); !errors.Is(err, ErrWebhookExpired) {
		t.Fatalf("ProcessWebhook() stale error = %v, want ErrWebhookExpired", err)
	}
}

func TestServiceMarksLatePaymentForRefundWithoutIssuingTickets(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	currentTime := now.Add(-2 * time.Minute)
	repository := NewMemoryRepository([]Order{testPendingOrder(now.Add(-time.Minute))})
	provider := NewFakeProvider(testWebhookSecret, 5*time.Minute)
	service := NewService(repository, provider, func() time.Time { return currentTime })

	intent, err := service.CreatePaymentIntent(context.Background(), testOrderID, testUserID)
	if err != nil {
		t.Fatalf("CreatePaymentIntent() error = %v", err)
	}
	currentTime = now
	payload := provider.SuccessPayload("evt-payment-late", intent.Reference, now)
	if err := service.ProcessWebhook(context.Background(), signedWebhookRequest(t, provider, payload, now)); err != nil {
		t.Fatalf("ProcessWebhook() error = %v", err)
	}

	order, ok := repository.Order(testOrderID)
	if !ok || order.Status != OrderExpired || len(order.Tickets) != 0 {
		t.Fatalf("order = %#v, want expired order without tickets", order)
	}
	payment, ok := repository.Payment(testOrderID)
	if !ok || payment.Status != PaymentRefundPending {
		t.Fatalf("payment = %#v, want refund pending", payment)
	}
}

func TestServiceHandlesFailedExpiredAndOutOfOrderEvents(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	repository := NewMemoryRepository([]Order{testPendingOrder(now.Add(time.Minute))})
	provider := NewFakeProvider(testWebhookSecret, 5*time.Minute)
	service := NewService(repository, provider, func() time.Time { return now })
	intent, err := service.CreatePaymentIntent(context.Background(), testOrderID, testUserID)
	if err != nil {
		t.Fatalf("CreatePaymentIntent() error = %v", err)
	}

	failed := provider.EventPayload("evt-payment-failed", "payment.failed", intent.Reference, now)
	if err := service.ProcessWebhook(context.Background(), signedWebhookRequest(t, provider, failed, now)); err != nil {
		t.Fatalf("ProcessWebhook() failed event error = %v", err)
	}
	if payment, ok := repository.Payment(testOrderID); !ok || payment.Status != PaymentFailed {
		t.Fatalf("payment after failure = %#v, want failed", payment)
	}

	succeeded := provider.SuccessPayload("evt-payment-succeeded", intent.Reference, now)
	if err := service.ProcessWebhook(context.Background(), signedWebhookRequest(t, provider, succeeded, now)); err != nil {
		t.Fatalf("ProcessWebhook() later success error = %v", err)
	}
	expired := provider.EventPayload("evt-payment-expired", "payment.expired", intent.Reference, now)
	if err := service.ProcessWebhook(context.Background(), signedWebhookRequest(t, provider, expired, now)); err != nil {
		t.Fatalf("ProcessWebhook() out-of-order expiry error = %v", err)
	}

	order, ok := repository.Order(testOrderID)
	if !ok || order.Status != OrderPaid || len(order.Tickets) != 1 {
		t.Fatalf("order = %#v, want paid order with one ticket", order)
	}
	payment, ok := repository.Payment(testOrderID)
	if !ok || payment.Status != PaymentSucceeded {
		t.Fatalf("payment after out-of-order events = %#v, want succeeded", payment)
	}
}

func signedWebhookRequest(t *testing.T, provider *FakeProvider, payload []byte, timestamp time.Time) WebhookRequest {
	t.Helper()
	return WebhookRequest{
		Body: payload,
		Header: http.Header{
			"X-Payment-Timestamp": []string{timestamp.Format(time.RFC3339)},
			"X-Payment-Signature": []string{provider.Sign(payload, timestamp)},
		},
	}
}

const (
	testOrderID = "10000000-0000-4000-8000-000000000001"
	testUserID  = "40000000-0000-4000-8000-000000000001"
)

func testPendingOrder(expiresAt time.Time) Order {
	return Order{
		ID:        testOrderID,
		UserID:    testUserID,
		Status:    OrderPendingPayment,
		ExpiresAt: expiresAt,
		Items: []OrderItem{{
			ID:          "20000000-0000-4000-8000-000000000001",
			SeatID:      "30000000-0000-4000-8000-000000000001",
			PriceAmount: "50000.00",
			Currency:    "IDR",
		}},
	}
}

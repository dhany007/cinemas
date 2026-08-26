package payments

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"
)

const (
	fakeTimestampHeader = "X-Payment-Timestamp"
	fakeSignatureHeader = "X-Payment-Signature"
)

// FakeProvider is a deterministic adapter intended only for local development
// and tests. It creates pending intents; callers must submit a signed webhook
// to complete the simulated payment.
type FakeProvider struct {
	webhookSecret []byte
	replayWindow  time.Duration
}

func NewFakeProvider(webhookSecret string, replayWindow time.Duration) *FakeProvider {
	return &FakeProvider{webhookSecret: []byte(webhookSecret), replayWindow: replayWindow}
}

func (p *FakeProvider) Name() string {
	return FakeProviderName
}

func (p *FakeProvider) CreatePaymentIntent(_ context.Context, request PaymentIntentRequest) (PaymentIntent, error) {
	return PaymentIntent{
		Provider:  FakeProviderName,
		Reference: "fake-" + request.OrderID,
		Status:    PaymentPending,
		Amount:    request.Amount,
		Currency:  request.Currency,
	}, nil
}

func (p *FakeProvider) VerifyWebhook(request WebhookRequest, now time.Time) (WebhookEvent, error) {
	timestamp, err := time.Parse(time.RFC3339, request.Header.Get(fakeTimestampHeader))
	if err != nil {
		return WebhookEvent{}, ErrInvalidWebhookSignature
	}
	if timestamp.Before(now.Add(-p.replayWindow)) || timestamp.After(now.Add(p.replayWindow)) {
		return WebhookEvent{}, ErrWebhookExpired
	}
	providedSignature, err := hex.DecodeString(request.Header.Get(fakeSignatureHeader))
	if err != nil {
		return WebhookEvent{}, ErrInvalidWebhookSignature
	}
	mac := hmac.New(sha256.New, p.webhookSecret)
	_, _ = mac.Write([]byte(timestamp.UTC().Format(time.RFC3339)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(request.Body)
	if !hmac.Equal(providedSignature, mac.Sum(nil)) {
		return WebhookEvent{}, ErrInvalidWebhookSignature
	}

	var payload struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Reference  string `json:"reference"`
		OccurredAt string `json:"occurred_at"`
	}
	if err := json.Unmarshal(request.Body, &payload); err != nil {
		return WebhookEvent{}, ErrInvalidWebhookSignature
	}
	occurredAt, err := time.Parse(time.RFC3339, payload.OccurredAt)
	if err != nil || strings.TrimSpace(payload.ID) == "" || strings.TrimSpace(payload.Reference) == "" {
		return WebhookEvent{}, ErrInvalidWebhookSignature
	}
	status, ok := webhookStatus(payload.Type)
	if !ok {
		return WebhookEvent{}, ErrInvalidWebhookSignature
	}
	return WebhookEvent{
		Provider:          FakeProviderName,
		ProviderEventID:   payload.ID,
		ProviderReference: payload.Reference,
		Status:            status,
		OccurredAt:        occurredAt.UTC(),
	}, nil
}

func (p *FakeProvider) SuccessPayload(eventID, reference string, occurredAt time.Time) []byte {
	return p.EventPayload(eventID, "payment.succeeded", reference, occurredAt)
}

// EventPayload creates a signed-webhook-compatible fake event payload for
// local development and tests.
func (p *FakeProvider) EventPayload(eventID, eventType, reference string, occurredAt time.Time) []byte {
	payload, _ := json.Marshal(struct {
		ID         string `json:"id"`
		Type       string `json:"type"`
		Reference  string `json:"reference"`
		OccurredAt string `json:"occurred_at"`
	}{eventID, eventType, reference, occurredAt.UTC().Format(time.RFC3339)})
	return payload
}

func (p *FakeProvider) Sign(body []byte, timestamp time.Time) string {
	mac := hmac.New(sha256.New, p.webhookSecret)
	_, _ = mac.Write([]byte(timestamp.UTC().Format(time.RFC3339)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func webhookStatus(eventType string) (WebhookEventStatus, bool) {
	switch eventType {
	case "payment.succeeded":
		return WebhookPaymentSucceeded, true
	case "payment.failed":
		return WebhookPaymentFailed, true
	case "payment.expired":
		return WebhookPaymentExpired, true
	default:
		return "", false
	}
}

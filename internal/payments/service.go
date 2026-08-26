package payments

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Service coordinates payment intents and verified provider webhooks.
type Service struct {
	repository Repository
	provider   Provider
	clock      func() time.Time
}

func NewService(repository Repository, provider Provider, clock func() time.Time) *Service {
	return &Service{repository: repository, provider: provider, clock: clock}
}

// ProviderName identifies the webhook path accepted by this service.
func (s *Service) ProviderName() string {
	return s.provider.Name()
}

// CreatePaymentIntent asks the provider for an intent but never marks an order
// paid. Only ProcessWebhook may transition an order to PAID.
func (s *Service) CreatePaymentIntent(ctx context.Context, orderID, userID string) (PaymentIntent, error) {
	if err := ctx.Err(); err != nil {
		return PaymentIntent{}, err
	}
	if strings.TrimSpace(orderID) == "" || strings.TrimSpace(userID) == "" {
		return PaymentIntent{}, ErrOrderNotFound
	}

	now := s.clock().UTC()
	request, existing, err := s.repository.PaymentIntentRequest(ctx, orderID, userID, now)
	if err != nil {
		return PaymentIntent{}, fmt.Errorf("prepare payment intent: %w", err)
	}
	if existing != nil {
		return *existing, nil
	}
	intent, err := s.provider.CreatePaymentIntent(ctx, request)
	if err != nil {
		return PaymentIntent{}, fmt.Errorf("create provider payment intent: %w", err)
	}
	if intent.Provider != s.provider.Name() || intent.Reference == "" || intent.Status != PaymentPending {
		return PaymentIntent{}, fmt.Errorf("create provider payment intent: invalid provider response")
	}
	intent, err = s.repository.SavePaymentIntent(ctx, intent, orderID, now)
	if err != nil {
		return PaymentIntent{}, fmt.Errorf("save payment intent: %w", err)
	}
	return intent, nil
}

// ProcessWebhook verifies the provider signature and replay window before any
// durable state is changed.
func (s *Service) ProcessWebhook(ctx context.Context, request WebhookRequest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	event, err := s.provider.VerifyWebhook(request, s.clock().UTC())
	if err != nil {
		return fmt.Errorf("verify payment webhook: %w", err)
	}
	if event.Provider != s.provider.Name() {
		return fmt.Errorf("verify payment webhook: provider mismatch")
	}
	if err := s.repository.ProcessWebhookEvent(ctx, event, s.clock().UTC()); err != nil {
		return fmt.Errorf("process payment webhook: %w", err)
	}
	return nil
}

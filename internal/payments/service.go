package payments

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Service creates development fake payments.
type Service struct {
	repository Repository
	clock      func() time.Time
}

// NewService creates a fake-payment service backed by the supplied repository.
func NewService(repository Repository, clock func() time.Time) *Service {
	return &Service{repository: repository, clock: clock}
}

// CreateFakePayment completes an eligible order without an external gateway call.
func (s *Service) CreateFakePayment(ctx context.Context, orderID string) (Payment, error) {
	if err := ctx.Err(); err != nil {
		return Payment{}, err
	}
	if strings.TrimSpace(orderID) == "" {
		return Payment{}, ErrOrderNotFound
	}

	payment, err := s.repository.CompleteFakePayment(ctx, orderID, s.clock().UTC())
	if err != nil {
		return Payment{}, fmt.Errorf("complete fake payment: %w", err)
	}
	return payment, nil
}

package tickets

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Service retrieves customer tickets and delivers committed ticket events.
type Service struct {
	repository Repository
	notifier   Notifier
	clock      func() time.Time
	retryDelay time.Duration
	lease      time.Duration
}

func NewService(repository Repository, notifier Notifier, clock func() time.Time, retryDelay time.Duration) *Service {
	return &Service{repository: repository, notifier: notifier, clock: clock, retryDelay: retryDelay, lease: retryDelay}
}

func (s *Service) ListOrderTickets(ctx context.Context, orderID, userID string) ([]Ticket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(orderID) == "" || strings.TrimSpace(userID) == "" {
		return nil, ErrOrderNotFound
	}
	tickets, err := s.repository.ListOrderTickets(ctx, orderID, userID)
	if err != nil {
		return nil, fmt.Errorf("list order tickets: %w", err)
	}
	return tickets, nil
}

// DeliverPending processes a bounded batch. A failed notifier call is retained
// for retry; delivery failures do not stop unrelated events in the batch.
func (s *Service) DeliverPending(ctx context.Context, limit int) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if s.notifier == nil || limit < 1 {
		return 0, nil
	}
	now := s.clock().UTC()
	events, err := s.repository.ClaimTicketDeliveries(ctx, now, limit, s.lease)
	if err != nil {
		return 0, fmt.Errorf("claim ticket deliveries: %w", err)
	}
	completed := 0
	for _, event := range events {
		delivery, err := s.repository.LoadDelivery(ctx, event)
		if err == nil {
			err = s.notifier.Deliver(ctx, delivery)
		}
		if err != nil {
			if retryErr := s.repository.RetryTicketDelivery(ctx, event.ID, now, s.retryDelay); retryErr != nil {
				return completed, fmt.Errorf("reschedule ticket delivery %d after %v: %w", event.ID, err, retryErr)
			}
			continue
		}
		if err := s.repository.CompleteTicketDelivery(ctx, event.ID, now); err != nil {
			return completed, fmt.Errorf("complete ticket delivery %d: %w", event.ID, err)
		}
		completed++
	}
	return completed, nil
}

package booking

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	uuidByteLength   = 16
	uuidVersionIndex = 6
	uuidVariantIndex = 8
	uuidVersionMask  = 0x0f
	uuidVersion4     = 0x40
	uuidVariantMask  = 0x3f
	uuidVariantRFC   = 0x80
)

// Repository persists idempotent order holds and exposes existing orders.
type Repository interface {
	FindOrderByIdempotency(ctx context.Context, userID, key string) (Order, bool, error)
	CreateHold(ctx context.Context, order Order, now time.Time) (Order, error)
}

// Service applies the business rules for booking seat holds.
type Service struct {
	repository   Repository
	holdDuration time.Duration
	clock        func() time.Time
	newID        func() (string, error)
}

// NewService creates a booking service with the supplied persistence and clock.
func NewService(repository Repository, holdDuration time.Duration, clock func() time.Time) *Service {
	return &Service{
		repository:   repository,
		holdDuration: holdDuration,
		clock:        clock,
		newID:        randomID,
	}
}

// CreateHold reserves every requested seat or returns a conflict without a partial hold.
func (s *Service) CreateHold(ctx context.Context, input CreateHoldInput) (Order, error) {
	if err := ctx.Err(); err != nil {
		return Order{}, err
	}

	seatIDs, err := validateAndSortSeatIDs(input)
	if err != nil {
		return Order{}, err
	}

	existing, found, err := s.repository.FindOrderByIdempotency(ctx, input.UserID, input.IdempotencyKey)
	if err != nil {
		return Order{}, fmt.Errorf("find idempotent order: %w", err)
	}
	if found {
		if sameHoldRequest(existing, input.ShowtimeID, seatIDs) {
			return existing, nil
		}
		return Order{}, ErrIdempotencyKeyReused
	}

	id, err := s.newID()
	if err != nil {
		return Order{}, fmt.Errorf("generate order id: %w", err)
	}

	now := s.clock().UTC()
	order := Order{
		ID:             id,
		UserID:         input.UserID,
		ShowtimeID:     input.ShowtimeID,
		IdempotencyKey: input.IdempotencyKey,
		Status:         OrderPendingPayment,
		ExpiresAt:      now.Add(s.holdDuration),
		Items:          makeOrderItems(seatIDs),
	}

	created, err := s.repository.CreateHold(ctx, order, now)
	if err != nil {
		return Order{}, fmt.Errorf("create seat hold: %w", err)
	}
	return created, nil
}

func validateAndSortSeatIDs(input CreateHoldInput) ([]string, error) {
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.ShowtimeID) == "" ||
		strings.TrimSpace(input.IdempotencyKey) == "" || len(input.SeatIDs) == 0 {
		return nil, ErrInvalidHoldInput
	}

	seen := make(map[string]struct{}, len(input.SeatIDs))
	seatIDs := make([]string, 0, len(input.SeatIDs))
	for _, seatID := range input.SeatIDs {
		seatID = strings.TrimSpace(seatID)
		if seatID == "" {
			return nil, ErrInvalidHoldInput
		}
		if _, exists := seen[seatID]; exists {
			return nil, ErrInvalidHoldInput
		}
		seen[seatID] = struct{}{}
		seatIDs = append(seatIDs, seatID)
	}
	sort.Strings(seatIDs)
	return seatIDs, nil
}

func makeOrderItems(seatIDs []string) []OrderItem {
	items := make([]OrderItem, len(seatIDs))
	for i, seatID := range seatIDs {
		items[i] = OrderItem{SeatID: seatID}
	}
	return items
}

func sameHoldRequest(order Order, showtimeID string, seatIDs []string) bool {
	if order.ShowtimeID != showtimeID || len(order.Items) != len(seatIDs) {
		return false
	}
	for i, item := range order.Items {
		if item.SeatID != seatIDs[i] {
			return false
		}
	}
	return true
}

func randomID() (string, error) {
	bytes := make([]byte, uuidByteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[uuidVersionIndex] = bytes[uuidVersionIndex]&uuidVersionMask | uuidVersion4
	bytes[uuidVariantIndex] = bytes[uuidVariantIndex]&uuidVariantMask | uuidVariantRFC
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]),
		hex.EncodeToString(bytes[4:6]),
		hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]),
		hex.EncodeToString(bytes[10:16]),
	), nil
}

package seatinventory

import (
	"context"
	"fmt"
	"strings"
)

// Service provides public seat-map availability.
type Service struct {
	repository Repository
}

// NewService creates a seat-map service backed by the supplied repository.
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// ListSeatMap returns the public inventory of a showtime.
func (s *Service) ListSeatMap(ctx context.Context, showtimeID string) ([]Seat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(showtimeID) == "" {
		return nil, ErrShowtimeNotFound
	}

	seats, err := s.repository.ListSeatMap(ctx, showtimeID)
	if err != nil {
		return nil, fmt.Errorf("list seat map: %w", err)
	}
	return seats, nil
}

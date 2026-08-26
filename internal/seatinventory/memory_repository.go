package seatinventory

import (
	"context"
	"sync"
)

// MemoryRepository is a concurrency-safe seat-map repository for tests.
type MemoryRepository struct {
	mu        sync.RWMutex
	showtimes map[string][]Seat
}

// NewMemoryRepository creates a memory repository seeded with showtime seat maps.
func NewMemoryRepository(showtimes map[string][]Seat) *MemoryRepository {
	repository := &MemoryRepository{showtimes: make(map[string][]Seat, len(showtimes))}
	for showtimeID, seats := range showtimes {
		repository.showtimes[showtimeID] = copySeats(seats)
	}
	return repository
}

// ListSeatMap returns a defensive copy of a test showtime seat map.
func (r *MemoryRepository) ListSeatMap(_ context.Context, showtimeID string) ([]Seat, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seats, ok := r.showtimes[showtimeID]
	if !ok {
		return nil, ErrShowtimeNotFound
	}
	return copySeats(seats), nil
}

func copySeats(seats []Seat) []Seat {
	return append([]Seat(nil), seats...)
}

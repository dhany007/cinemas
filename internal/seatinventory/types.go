package seatinventory

import (
	"context"
	"errors"
)

// ErrShowtimeNotFound indicates that the requested showtime does not exist.
var ErrShowtimeNotFound = errors.New("showtime not found")

// Seat is the public availability and price of one seat for a showtime.
type Seat struct {
	ID          string
	RowLabel    string
	SeatNumber  string
	SeatType    string
	PriceAmount string
	Currency    string
	Status      string
}

// Repository lists public seat-map inventory for showtimes.
type Repository interface {
	ListSeatMap(ctx context.Context, showtimeID string) ([]Seat, error)
}

package seatinventory

import (
	"context"
	"errors"
	"testing"
)

func TestServiceListSeatMap(t *testing.T) {
	repository := NewMemoryRepository(map[string][]Seat{
		"showtime-1": {
			{
				ID:          "seat-a",
				RowLabel:    "A",
				SeatNumber:  "1",
				SeatType:    "STANDARD",
				PriceAmount: "50000.00",
				Currency:    "IDR",
				Status:      "AVAILABLE",
			},
		},
	})
	service := NewService(repository)

	seats, err := service.ListSeatMap(context.Background(), "showtime-1")
	if err != nil {
		t.Fatalf("ListSeatMap() error = %v", err)
	}
	if len(seats) != 1 {
		t.Fatalf("seat count = %d, want 1", len(seats))
	}
	if seats[0].PriceAmount != "50000.00" || seats[0].Status != "AVAILABLE" {
		t.Fatalf("seat = %#v, want price and status", seats[0])
	}
}

func TestServiceListSeatMapReturnsNotFound(t *testing.T) {
	service := NewService(NewMemoryRepository(nil))

	_, err := service.ListSeatMap(context.Background(), "missing-showtime")
	if !errors.Is(err, ErrShowtimeNotFound) {
		t.Fatalf("ListSeatMap() error = %v, want ErrShowtimeNotFound", err)
	}
}

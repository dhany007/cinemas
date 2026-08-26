package scheduling

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceListMovieShowtimes(t *testing.T) {
	movieID := "10000000-0000-4000-8000-000000000001"
	date := time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC)
	service := NewService(NewMemoryRepository(map[string][]Showtime{
		movieID: {
			{
				ID:         "20000000-0000-4000-8000-000000000001",
				StudioID:   "30000000-0000-4000-8000-000000000001",
				StudioName: "Studio 1",
				CinemaID:   "40000000-0000-4000-8000-000000000001",
				CinemaName: "Central Cinema",
				CinemaCity: "Jakarta",
				StartsAt:   date.Add(10 * time.Hour),
				EndsAt:     date.Add(12 * time.Hour),
				BasePrice:  "50000.00",
				Currency:   "IDR",
			},
		},
	}))

	showtimes, err := service.ListMovieShowtimes(context.Background(), ListInput{
		MovieID: movieID,
		Date:    date,
	})
	if err != nil {
		t.Fatalf("ListMovieShowtimes() error = %v", err)
	}
	if len(showtimes) != 1 {
		t.Fatalf("showtime count = %d, want 1", len(showtimes))
	}
	if showtimes[0].CinemaName != "Central Cinema" || showtimes[0].BasePrice != "50000.00" {
		t.Fatalf("showtime = %#v, want cinema and price", showtimes[0])
	}
}

func TestServiceListMovieShowtimesReturnsNotFound(t *testing.T) {
	service := NewService(NewMemoryRepository(nil))

	_, err := service.ListMovieShowtimes(context.Background(), ListInput{
		MovieID: "10000000-0000-4000-8000-000000000002",
		Date:    time.Date(2026, time.August, 26, 0, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, ErrMovieNotFound) {
		t.Fatalf("ListMovieShowtimes() error = %v, want ErrMovieNotFound", err)
	}
}

func TestParseDate(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		wantErr bool
	}{
		{name: "valid", value: "2026-08-26"},
		{name: "missing", wantErr: true},
		{name: "invalid", value: "26-08-2026", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			date, err := ParseDate(test.value)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidDate) {
					t.Fatalf("ParseDate() error = %v, want ErrInvalidDate", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseDate() error = %v", err)
			}
			if got := date.Format(time.DateOnly); got != test.value {
				t.Fatalf("date = %q, want %q", got, test.value)
			}
		})
	}
}

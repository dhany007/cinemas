package scheduling

import (
	"context"
	"fmt"
	"time"
)

// Service provides public movie-showtime queries.
type Service struct {
	repository Repository
}

// NewService creates a movie-showtime service backed by the supplied repository.
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// ParseDate parses a required UTC calendar date from the public API.
func ParseDate(value string) (time.Time, error) {
	date, err := time.Parse(time.DateOnly, value)
	if err != nil {
		return time.Time{}, ErrInvalidDate
	}
	return date, nil
}

// ListMovieShowtimes returns public screenings for one movie and one UTC date.
func (s *Service) ListMovieShowtimes(ctx context.Context, input ListInput) ([]Showtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.MovieID == "" || input.Date.IsZero() {
		return nil, ErrInvalidDate
	}

	showtimes, err := s.repository.ListMovieShowtimes(ctx, ListInput{
		MovieID: input.MovieID,
		Date:    startOfDayUTC(input.Date),
	})
	if err != nil {
		return nil, fmt.Errorf("list movie showtimes: %w", err)
	}
	return showtimes, nil
}

func startOfDayUTC(value time.Time) time.Time {
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

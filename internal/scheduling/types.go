package scheduling

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrMovieNotFound indicates that the requested movie does not exist.
	ErrMovieNotFound = errors.New("movie not found")
	// ErrInvalidDate indicates that a showtime date is absent or malformed.
	ErrInvalidDate = errors.New("invalid date")
)

// Showtime is the public screening and venue detail for one movie.
type Showtime struct {
	ID         string
	StudioID   string
	StudioName string
	CinemaID   string
	CinemaName string
	CinemaCity string
	StartsAt   time.Time
	EndsAt     time.Time
	BasePrice  string
	Currency   string
}

// ListInput contains the movie and UTC calendar date for a showtime lookup.
type ListInput struct {
	MovieID string
	Date    time.Time
}

// Repository lists showtimes for public movie browsing.
type Repository interface {
	ListMovieShowtimes(ctx context.Context, input ListInput) ([]Showtime, error)
}

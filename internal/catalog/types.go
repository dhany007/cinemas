package catalog

import (
	"context"
	"errors"
	"time"
)

const (
	// DefaultPageSize is used when a movie-list request omits its limit.
	DefaultPageSize = 20
	maxPageSize     = 100
)

var (
	// ErrInvalidCursor indicates that a list cursor cannot be decoded safely.
	ErrInvalidCursor = errors.New("invalid cursor")
	// ErrInvalidPageSize indicates that a list limit is outside the supported range.
	ErrInvalidPageSize = errors.New("invalid page size")
)

// Movie is the public metadata for one film.
type Movie struct {
	ID              string
	Title           string
	DurationMinutes int
	Rating          *string
	Synopsis        *string
	PosterURL       *string
	ReleaseDate     *string
	CreatedAt       time.Time
}

// Cursor identifies the final movie on a keyset page.
type Cursor struct {
	CreatedAt time.Time
	ID        string
}

// ListInput contains caller-provided movie pagination values.
type ListInput struct {
	Limit  int
	Cursor string
}

// ListQuery is the repository query after the cursor has been validated.
type ListQuery struct {
	Limit  int
	Cursor *Cursor
}

// Page is one ordered page of public movie metadata.
type Page struct {
	Movies     []Movie
	NextCursor string
}

// Repository lists public movie metadata in deterministic order.
type Repository interface {
	ListMovies(ctx context.Context, query ListQuery) ([]Movie, error)
}

package scheduling

import (
	"context"
	"sort"
	"time"
)

// MemoryRepository is a deterministic movie-showtime repository for tests.
type MemoryRepository struct {
	movies map[string][]Showtime
}

// NewMemoryRepository creates a memory repository seeded with movie showtimes.
func NewMemoryRepository(movies map[string][]Showtime) *MemoryRepository {
	repository := &MemoryRepository{movies: make(map[string][]Showtime, len(movies))}
	for movieID, showtimes := range movies {
		repository.movies[movieID] = append([]Showtime(nil), showtimes...)
		sortShowtimes(repository.movies[movieID])
	}
	return repository
}

// ListMovieShowtimes returns the selected UTC calendar day from a test fixture.
func (r *MemoryRepository) ListMovieShowtimes(_ context.Context, input ListInput) ([]Showtime, error) {
	showtimes, ok := r.movies[input.MovieID]
	if !ok {
		return nil, ErrMovieNotFound
	}

	matchingShowtimes := make([]Showtime, 0, len(showtimes))
	for _, showtime := range showtimes {
		if isOnDate(showtime, input.Date) {
			matchingShowtimes = append(matchingShowtimes, showtime)
		}
	}
	return matchingShowtimes, nil
}

func sortShowtimes(showtimes []Showtime) {
	sort.Slice(showtimes, func(left, right int) bool {
		if showtimes[left].StartsAt.Equal(showtimes[right].StartsAt) {
			return showtimes[left].ID < showtimes[right].ID
		}
		return showtimes[left].StartsAt.Before(showtimes[right].StartsAt)
	})
}

func isOnDate(showtime Showtime, dateStart time.Time) bool {
	dateEnd := dateStart.AddDate(0, 0, 1)
	return !showtime.StartsAt.Before(dateStart) && showtime.StartsAt.Before(dateEnd)
}

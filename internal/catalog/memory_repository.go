package catalog

import (
	"context"
	"sort"
)

// MemoryRepository is a deterministic movie repository for tests.
type MemoryRepository struct {
	movies []Movie
}

// NewMemoryRepository creates a memory repository seeded with public movies.
func NewMemoryRepository(movies []Movie) *MemoryRepository {
	repository := &MemoryRepository{movies: append([]Movie(nil), movies...)}
	sortMovies(repository.movies)
	return repository
}

// ListMovies returns a keyset page from its fixed test fixture.
func (r *MemoryRepository) ListMovies(_ context.Context, query ListQuery) ([]Movie, error) {
	movies := make([]Movie, 0, query.Limit)
	for _, movie := range r.movies {
		if query.Cursor != nil && !isAfterCursor(movie, *query.Cursor) {
			continue
		}
		movies = append(movies, movie)
		if len(movies) == query.Limit {
			break
		}
	}
	return movies, nil
}

func sortMovies(movies []Movie) {
	sort.Slice(movies, func(left, right int) bool {
		if movies[left].CreatedAt.Equal(movies[right].CreatedAt) {
			return movies[left].ID > movies[right].ID
		}
		return movies[left].CreatedAt.After(movies[right].CreatedAt)
	})
}

func isAfterCursor(movie Movie, cursor Cursor) bool {
	return movie.CreatedAt.Before(cursor.CreatedAt) || movie.CreatedAt.Equal(cursor.CreatedAt) && movie.ID < cursor.ID
}

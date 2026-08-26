package postgres

import (
	"context"
	"fmt"

	"github.com/citradigital/cinemas/internal/catalog"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const listMoviesColumns = `
SELECT
    id::text,
    title,
    duration_minutes,
    rating,
    synopsis,
    poster_url,
    release_date::text,
    created_at
FROM movies`

// MoviesRepository reads public movie metadata from PostgreSQL.
type MoviesRepository struct {
	pool *pgxpool.Pool
}

// NewMoviesRepository creates a PostgreSQL-backed movie repository.
func NewMoviesRepository(pool *pgxpool.Pool) *MoviesRepository {
	return &MoviesRepository{pool: pool}
}

// ListMovies returns a deterministic keyset page ordered by creation time and identifier.
func (r *MoviesRepository) ListMovies(ctx context.Context, query catalog.ListQuery) ([]catalog.Movie, error) {
	rows, err := r.queryMovies(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query movies: %w", err)
	}
	defer rows.Close()

	movies := make([]catalog.Movie, 0, query.Limit)
	for rows.Next() {
		var movie catalog.Movie
		if err := rows.Scan(
			&movie.ID,
			&movie.Title,
			&movie.DurationMinutes,
			&movie.Rating,
			&movie.Synopsis,
			&movie.PosterURL,
			&movie.ReleaseDate,
			&movie.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan movie: %w", err)
		}
		movies = append(movies, movie)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate movies: %w", err)
	}
	return movies, nil
}

func (r *MoviesRepository) queryMovies(ctx context.Context, query catalog.ListQuery) (pgx.Rows, error) {
	if query.Cursor == nil {
		return r.pool.Query(ctx, listMoviesColumns+`
ORDER BY created_at DESC, id DESC
LIMIT $1`, query.Limit)
	}

	return r.pool.Query(ctx, listMoviesColumns+`
WHERE (created_at, id) < ($1, $2)
ORDER BY created_at DESC, id DESC
LIMIT $3`, query.Cursor.CreatedAt, query.Cursor.ID, query.Limit)
}

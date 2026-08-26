package postgres

import (
	"context"
	"fmt"

	"github.com/citradigital/cinemas/internal/scheduling"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ShowtimesRepository reads public movie showtimes from PostgreSQL.
type ShowtimesRepository struct {
	pool *pgxpool.Pool
}

// NewShowtimesRepository creates a PostgreSQL-backed showtimes repository.
func NewShowtimesRepository(pool *pgxpool.Pool) *ShowtimesRepository {
	return &ShowtimesRepository{pool: pool}
}

// ListMovieShowtimes returns all screenings for a movie on one UTC calendar day.
func (r *ShowtimesRepository) ListMovieShowtimes(
	ctx context.Context,
	input scheduling.ListInput,
) ([]scheduling.Showtime, error) {
	var exists bool
	if err := r.pool.QueryRow(
		ctx,
		"SELECT EXISTS (SELECT 1 FROM movies WHERE id = $1)",
		input.MovieID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check movie existence: %w", err)
	}
	if !exists {
		return nil, scheduling.ErrMovieNotFound
	}

	dateEnd := input.Date.AddDate(0, 0, 1)
	rows, err := r.pool.Query(ctx, `
SELECT
    showtimes.id::text,
    studios.id::text,
    studios.name,
    cinemas.id::text,
    cinemas.name,
    cinemas.city,
    showtimes.starts_at,
    showtimes.ends_at,
    showtimes.base_price::text,
    showtimes.currency::text
FROM showtimes
JOIN studios ON studios.id = showtimes.studio_id
JOIN cinemas ON cinemas.id = studios.cinema_id
WHERE showtimes.movie_id = $1
  AND showtimes.starts_at >= $2
  AND showtimes.starts_at < $3
ORDER BY showtimes.starts_at, showtimes.id`, input.MovieID, input.Date, dateEnd)
	if err != nil {
		return nil, fmt.Errorf("query movie showtimes: %w", err)
	}
	defer rows.Close()

	showtimes := make([]scheduling.Showtime, 0)
	for rows.Next() {
		var showtime scheduling.Showtime
		if err := rows.Scan(
			&showtime.ID,
			&showtime.StudioID,
			&showtime.StudioName,
			&showtime.CinemaID,
			&showtime.CinemaName,
			&showtime.CinemaCity,
			&showtime.StartsAt,
			&showtime.EndsAt,
			&showtime.BasePrice,
			&showtime.Currency,
		); err != nil {
			return nil, fmt.Errorf("scan movie showtime: %w", err)
		}
		showtimes = append(showtimes, showtime)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate movie showtimes: %w", err)
	}
	return showtimes, nil
}

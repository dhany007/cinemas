package postgres

import (
	"context"
	"fmt"

	"github.com/citradigital/cinemas/internal/seatinventory"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeatMapRepository reads public showtime inventory from PostgreSQL.
type SeatMapRepository struct {
	pool *pgxpool.Pool
}

// NewSeatMapRepository creates a PostgreSQL-backed seat-map repository.
func NewSeatMapRepository(pool *pgxpool.Pool) *SeatMapRepository {
	return &SeatMapRepository{pool: pool}
}

// ListSeatMap returns the seats for a showtime with expired holds shown as available.
func (r *SeatMapRepository) ListSeatMap(
	ctx context.Context,
	showtimeID string,
) ([]seatinventory.Seat, error) {
	var exists bool
	if err := r.pool.QueryRow(
		ctx,
		"SELECT EXISTS (SELECT 1 FROM showtimes WHERE id = $1)",
		showtimeID,
	).Scan(&exists); err != nil {
		return nil, fmt.Errorf("check showtime existence: %w", err)
	}
	if !exists {
		return nil, seatinventory.ErrShowtimeNotFound
	}

	rows, err := r.pool.Query(ctx, `
SELECT
    showtime_seats.id::text,
    seats.row_label,
    seats.seat_number,
    seats.seat_type,
    showtime_seats.price_amount::text,
    showtime_seats.currency::text,
    CASE
        WHEN showtime_seats.status = 'HELD'
            AND showtime_seats.hold_expires_at <= now() THEN 'AVAILABLE'
        ELSE showtime_seats.status
    END AS status
FROM showtime_seats
JOIN seats ON seats.id = showtime_seats.seat_id
WHERE showtime_seats.showtime_id = $1
ORDER BY seats.row_label, seats.seat_number, showtime_seats.id`, showtimeID)
	if err != nil {
		return nil, fmt.Errorf("query showtime seat map: %w", err)
	}
	defer rows.Close()

	seats := make([]seatinventory.Seat, 0)
	for rows.Next() {
		var seat seatinventory.Seat
		if err := rows.Scan(
			&seat.ID,
			&seat.RowLabel,
			&seat.SeatNumber,
			&seat.SeatType,
			&seat.PriceAmount,
			&seat.Currency,
			&seat.Status,
		); err != nil {
			return nil, fmt.Errorf("scan showtime seat: %w", err)
		}
		seats = append(seats, seat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate showtime seat map: %w", err)
	}
	return seats, nil
}

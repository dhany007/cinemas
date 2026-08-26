package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/citradigital/cinemas/internal/admin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminRepository persists administrator-managed resources in PostgreSQL.
type AdminRepository struct {
	pool *pgxpool.Pool
}

// NewAdminRepository creates a PostgreSQL administrator repository.
func NewAdminRepository(pool *pgxpool.Pool) *AdminRepository {
	return &AdminRepository{pool: pool}
}

// CreateCinema stores a cinema and its audit event in one transaction.
func (r *AdminRepository) CreateCinema(
	ctx context.Context,
	cinema admin.Cinema,
	audit admin.AuditEvent,
) (admin.Cinema, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return admin.Cinema{}, fmt.Errorf("begin cinema create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(
		ctx,
		`INSERT INTO cinemas (id, name, address, city) VALUES ($1, $2, $3, $4)`,
		cinema.ID,
		cinema.Name,
		cinema.Address,
		cinema.City,
	); err != nil {
		return admin.Cinema{}, fmt.Errorf("insert cinema: %w", err)
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return admin.Cinema{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Cinema{}, fmt.Errorf("commit cinema create: %w", err)
	}
	return cinema, nil
}

// ListCinemas returns all cinemas ordered by name and ID.
func (r *AdminRepository) ListCinemas(ctx context.Context) ([]admin.Cinema, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id::text, name, address, city FROM cinemas ORDER BY name, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list cinemas: %w", err)
	}
	defer rows.Close()

	var cinemas []admin.Cinema
	for rows.Next() {
		cinema, err := scanCinema(rows)
		if err != nil {
			return nil, err
		}
		cinemas = append(cinemas, cinema)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate cinemas: %w", err)
	}
	return cinemas, nil
}

// FindCinema returns a cinema by ID.
func (r *AdminRepository) FindCinema(ctx context.Context, id string) (admin.Cinema, error) {
	var cinema admin.Cinema
	err := r.pool.QueryRow(
		ctx,
		`SELECT id::text, name, address, city FROM cinemas WHERE id = $1`,
		id,
	).Scan(&cinema.ID, &cinema.Name, &cinema.Address, &cinema.City)
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.Cinema{}, admin.ErrCinemaNotFound
	}
	if err != nil {
		return admin.Cinema{}, fmt.Errorf("find cinema: %w", err)
	}
	return cinema, nil
}

// UpdateCinema stores a cinema replacement and audit event in one transaction.
func (r *AdminRepository) UpdateCinema(
	ctx context.Context,
	cinema admin.Cinema,
	audit admin.AuditEvent,
) (admin.Cinema, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return admin.Cinema{}, fmt.Errorf("begin cinema update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	commandTag, err := tx.Exec(
		ctx,
		`UPDATE cinemas SET name = $2, address = $3, city = $4, updated_at = now() WHERE id = $1`,
		cinema.ID,
		cinema.Name,
		cinema.Address,
		cinema.City,
	)
	if err != nil {
		return admin.Cinema{}, fmt.Errorf("update cinema: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return admin.Cinema{}, admin.ErrCinemaNotFound
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return admin.Cinema{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Cinema{}, fmt.Errorf("commit cinema update: %w", err)
	}
	return cinema, nil
}

// DeleteCinema removes a cinema and creates its audit event in one transaction.
func (r *AdminRepository) DeleteCinema(ctx context.Context, id string, audit admin.AuditEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin cinema delete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	commandTag, err := tx.Exec(ctx, `DELETE FROM cinemas WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete cinema: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return admin.ErrCinemaNotFound
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit cinema delete: %w", err)
	}
	return nil
}

type cinemaScanner interface {
	Scan(...any) error
}

func scanCinema(row cinemaScanner) (admin.Cinema, error) {
	var cinema admin.Cinema
	if err := row.Scan(&cinema.ID, &cinema.Name, &cinema.Address, &cinema.City); err != nil {
		return admin.Cinema{}, fmt.Errorf("scan cinema: %w", err)
	}
	return cinema, nil
}

func insertCinemaAuditEvent(ctx context.Context, tx pgx.Tx, audit admin.AuditEvent) error {
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO audit_events (actor_user_id, entity_type, entity_id, action) VALUES ($1, $2, $3, $4)`,
		audit.ActorUserID,
		audit.EntityType,
		audit.EntityID,
		audit.Action,
	); err != nil {
		return fmt.Errorf("insert cinema audit event: %w", err)
	}
	return nil
}

// CreateStudio stores a studio and audit event atomically.
//
//nolint:lll // Repository interface parameters are explicit.
func (r *AdminRepository) CreateStudio(ctx context.Context, studio admin.Studio, audit admin.AuditEvent) (admin.Studio, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.Studio{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err = tx.Exec(ctx, `INSERT INTO studios (id, cinema_id, name) VALUES ($1,$2,$3)`, studio.ID, studio.CinemaID, studio.Name); err != nil { //nolint:lll // SQL mutation is kept together.
		return admin.Studio{}, err
	}
	if err = insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return admin.Studio{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return admin.Studio{}, err
	}
	return studio, nil
}

// ListStudios returns stored studios.
func (r *AdminRepository) ListStudios(ctx context.Context) ([]admin.Studio, error) {
	rows, err := r.pool.Query(ctx, `SELECT id::text,cinema_id::text,name FROM studios ORDER BY name,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []admin.Studio
	for rows.Next() {
		var s admin.Studio
		if err := rows.Scan(&s.ID, &s.CinemaID, &s.Name); err != nil {
			return nil, err
		}
		result = append(result, s)
	}
	return result, rows.Err()
}

// UpdateStudio replaces a studio and audit event atomically.
//
//nolint:lll // Repository interface parameters are explicit.
func (r *AdminRepository) UpdateStudio(ctx context.Context, studio admin.Studio, audit admin.AuditEvent) (admin.Studio, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return admin.Studio{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `UPDATE studios SET cinema_id=$2,name=$3,updated_at=now() WHERE id=$1`, studio.ID, studio.CinemaID, studio.Name) //nolint:lll // SQL mutation is kept together.
	if err != nil {
		return admin.Studio{}, err
	}
	if tag.RowsAffected() == 0 {
		return admin.Studio{}, admin.ErrStudioNotFound
	}
	if err = insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return admin.Studio{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return admin.Studio{}, err
	}
	return studio, nil
}

// DeleteStudio removes a studio and records its audit event atomically.
func (r *AdminRepository) DeleteStudio(ctx context.Context, id string, audit admin.AuditEvent) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM studios WHERE id=$1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return admin.ErrStudioNotFound
	}
	if err = insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CreateSeat stores a physical seat and its audit event atomically.
func (r *AdminRepository) CreateSeat(ctx context.Context, seat admin.Seat, audit admin.AuditEvent) (admin.Seat, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return admin.Seat{}, fmt.Errorf("begin seat create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO seats (id, studio_id, row_label, seat_number, seat_type) VALUES ($1, $2, $3, $4, $5)`,
		seat.ID,
		seat.StudioID,
		seat.RowLabel,
		seat.SeatNumber,
		seat.SeatType,
	); err != nil {
		if isSeatLayoutConflict(err) {
			return admin.Seat{}, admin.ErrSeatAlreadyExists
		}
		if isForeignKeyViolation(err) {
			return admin.Seat{}, admin.ErrStudioNotFound
		}
		return admin.Seat{}, fmt.Errorf("insert seat: %w", err)
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return admin.Seat{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Seat{}, fmt.Errorf("commit seat create: %w", err)
	}
	return seat, nil
}

// ListSeats returns physical seats in stable layout order.
func (r *AdminRepository) ListSeats(ctx context.Context) ([]admin.Seat, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id::text, studio_id::text, row_label, seat_number, seat_type
		 FROM seats ORDER BY studio_id, row_label, seat_number, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list seats: %w", err)
	}
	defer rows.Close()
	var seats []admin.Seat
	for rows.Next() {
		var seat admin.Seat
		if err := rows.Scan(&seat.ID, &seat.StudioID, &seat.RowLabel, &seat.SeatNumber, &seat.SeatType); err != nil {
			return nil, fmt.Errorf("scan seat: %w", err)
		}
		seats = append(seats, seat)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate seats: %w", err)
	}
	return seats, nil
}

// UpdateSeat replaces a physical seat and its audit event atomically.
func (r *AdminRepository) UpdateSeat(ctx context.Context, seat admin.Seat, audit admin.AuditEvent) (admin.Seat, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return admin.Seat{}, fmt.Errorf("begin seat update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(
		ctx,
		`UPDATE seats
		 SET studio_id = $2, row_label = $3, seat_number = $4, seat_type = $5, updated_at = now()
		 WHERE id = $1`,
		seat.ID,
		seat.StudioID,
		seat.RowLabel,
		seat.SeatNumber,
		seat.SeatType,
	)
	if err != nil {
		if isSeatLayoutConflict(err) {
			return admin.Seat{}, admin.ErrSeatAlreadyExists
		}
		if isForeignKeyViolation(err) {
			return admin.Seat{}, admin.ErrStudioNotFound
		}
		return admin.Seat{}, fmt.Errorf("update seat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return admin.Seat{}, admin.ErrSeatNotFound
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return admin.Seat{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Seat{}, fmt.Errorf("commit seat update: %w", err)
	}
	return seat, nil
}

// DeleteSeat removes a physical seat and creates its audit event atomically.
func (r *AdminRepository) DeleteSeat(ctx context.Context, id string, audit admin.AuditEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin seat delete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM seats WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete seat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return admin.ErrSeatNotFound
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit seat delete: %w", err)
	}
	return nil
}

func isSeatLayoutConflict(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}

func isForeignKeyViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23503"
}

// CreateMovie stores a movie and its audit event atomically.
func (r *AdminRepository) CreateMovie(
	ctx context.Context,
	movie admin.Movie,
	audit admin.AuditEvent,
) (admin.Movie, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return admin.Movie{}, fmt.Errorf("begin movie create transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(
		ctx,
		`INSERT INTO movies (id, title, duration_minutes, rating, synopsis, poster_url, release_date)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		movie.ID,
		movie.Title,
		movie.DurationMinutes,
		movie.Rating,
		movie.Synopsis,
		movie.PosterURL,
		movie.ReleaseDate,
	); err != nil {
		return admin.Movie{}, fmt.Errorf("insert movie: %w", err)
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return admin.Movie{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Movie{}, fmt.Errorf("commit movie create: %w", err)
	}
	return movie, nil
}

// ListMovies returns movies in deterministic title and ID order.
func (r *AdminRepository) ListMovies(ctx context.Context) ([]admin.Movie, error) {
	rows, err := r.pool.Query(
		ctx,
		`SELECT id::text, title, duration_minutes, rating, synopsis, poster_url, release_date::text
		 FROM movies ORDER BY title, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("list movies: %w", err)
	}
	defer rows.Close()
	var movies []admin.Movie
	for rows.Next() {
		var movie admin.Movie
		if err := rows.Scan(
			&movie.ID,
			&movie.Title,
			&movie.DurationMinutes,
			&movie.Rating,
			&movie.Synopsis,
			&movie.PosterURL,
			&movie.ReleaseDate,
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

// UpdateMovie replaces a movie and its audit event atomically.
func (r *AdminRepository) UpdateMovie(
	ctx context.Context,
	movie admin.Movie,
	audit admin.AuditEvent,
) (admin.Movie, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return admin.Movie{}, fmt.Errorf("begin movie update transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(
		ctx,
		`UPDATE movies
		 SET title = $2, duration_minutes = $3, rating = $4, synopsis = $5, poster_url = $6, release_date = $7,
		     updated_at = now()
		 WHERE id = $1`,
		movie.ID,
		movie.Title,
		movie.DurationMinutes,
		movie.Rating,
		movie.Synopsis,
		movie.PosterURL,
		movie.ReleaseDate,
	)
	if err != nil {
		return admin.Movie{}, fmt.Errorf("update movie: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return admin.Movie{}, admin.ErrMovieNotFound
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return admin.Movie{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Movie{}, fmt.Errorf("commit movie update: %w", err)
	}
	return movie, nil
}

// DeleteMovie removes a movie and creates its audit event atomically.
func (r *AdminRepository) DeleteMovie(ctx context.Context, id string, audit admin.AuditEvent) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin movie delete transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tag, err := tx.Exec(ctx, `DELETE FROM movies WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete movie: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return admin.ErrMovieNotFound
	}
	if err := insertCinemaAuditEvent(ctx, tx, audit); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit movie delete: %w", err)
	}
	return nil
}

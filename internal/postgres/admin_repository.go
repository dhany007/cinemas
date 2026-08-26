package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/citradigital/cinemas/internal/admin"
	"github.com/jackc/pgx/v5"
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

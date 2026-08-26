package postgres

import (
	"context"
	"fmt"

	"github.com/citradigital/cinemas/internal/admin"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AdminRepository persists administrator-managed resources in PostgreSQL.
type AdminRepository struct{ pool *pgxpool.Pool }

// NewAdminRepository creates a PostgreSQL administrator repository.
func NewAdminRepository(pool *pgxpool.Pool) *AdminRepository { return &AdminRepository{pool: pool} }

// CreateCinema stores a cinema and its audit event in one transaction.
func (r *AdminRepository) CreateCinema(
	ctx context.Context,
	cinema admin.Cinema,
	audit admin.AuditEvent,
) (admin.Cinema, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return admin.Cinema{}, fmt.Errorf("begin cinema transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO cinemas (id, name, address, city) VALUES ($1, $2, $3, $4)`,
		cinema.ID, cinema.Name, cinema.Address, cinema.City); err != nil {
		return admin.Cinema{}, fmt.Errorf("insert cinema: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO audit_events (actor_user_id, entity_type, entity_id, action) VALUES ($1, $2, $3, $4)`,
		audit.ActorUserID, audit.EntityType, audit.EntityID, audit.Action); err != nil {
		return admin.Cinema{}, fmt.Errorf("insert audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Cinema{}, fmt.Errorf("commit cinema: %w", err)
	}
	return cinema, nil
}

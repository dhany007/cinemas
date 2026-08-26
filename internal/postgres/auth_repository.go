package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/citradigital/cinemas/internal/auth"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const adminBootstrapLockID int64 = 582647721

// AuthRepository persists passwords and roles in PostgreSQL.
type AuthRepository struct {
	pool *pgxpool.Pool
}

// NewAuthRepository creates a PostgreSQL-backed authentication repository.
func NewAuthRepository(pool *pgxpool.Pool) *AuthRepository {
	return &AuthRepository{pool: pool}
}

// CreateUser stores a customer account and its password hash atomically.
func (r *AuthRepository) CreateUser(ctx context.Context, user auth.NewUser) (auth.User, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return auth.User{}, fmt.Errorf("begin create user transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	created, err := insertUser(ctx, tx, user)
	if err != nil {
		return auth.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.User{}, fmt.Errorf("commit create user: %w", err)
	}
	return created, nil
}

// CreateInitialAdmin atomically permits only the first bootstrap administrator.
func (r *AuthRepository) CreateInitialAdmin(ctx context.Context, user auth.NewUser) (auth.User, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return auth.User{}, fmt.Errorf("begin bootstrap admin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock($1)", adminBootstrapLockID); err != nil {
		return auth.User{}, fmt.Errorf("lock admin bootstrap: %w", err)
	}
	var exists bool
	if err := tx.QueryRow(
		ctx,
		`SELECT EXISTS (SELECT 1 FROM user_roles WHERE role = $1)`,
		auth.RoleAdmin,
	).Scan(&exists); err != nil {
		return auth.User{}, fmt.Errorf("check initial admin: %w", err)
	}
	if exists {
		return auth.User{}, auth.ErrAdminAlreadyBootstrapped
	}
	created, err := insertUser(ctx, tx, user)
	if err != nil {
		return auth.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return auth.User{}, fmt.Errorf("commit bootstrap admin: %w", err)
	}
	return created, nil
}

// FindUserByEmail returns the profile, role, and password hash needed for password login.
func (r *AuthRepository) FindUserByEmail(ctx context.Context, email string) (auth.StoredUser, error) {
	var storedUser auth.StoredUser
	err := r.pool.QueryRow(ctx, `
SELECT u.id::text, u.email, u.display_name, ur.role, COALESCE(u.password_hash, '')
FROM users AS u
JOIN user_roles AS ur ON ur.user_id = u.id
WHERE u.email = $1 AND ur.role IN ($2, $3)
ORDER BY CASE ur.role WHEN $3 THEN 0 ELSE 1 END
LIMIT 1`, email, auth.RoleCustomer, auth.RoleAdmin).Scan(
		&storedUser.ID,
		&storedUser.Email,
		&storedUser.DisplayName,
		&storedUser.Role,
		&storedUser.PasswordHash,
	)
	if errors.Is(err, pgx.ErrNoRows) || storedUser.PasswordHash == "" {
		return auth.StoredUser{}, auth.ErrInvalidCredentials
	}
	if err != nil {
		return auth.StoredUser{}, fmt.Errorf("query user by email: %w", err)
	}
	return storedUser, nil
}

func insertUser(ctx context.Context, tx pgx.Tx, user auth.NewUser) (auth.User, error) {
	if _, err := tx.Exec(ctx, `
INSERT INTO users (id, email, display_name, password_hash)
VALUES ($1, $2, $3, $4)`, user.ID, user.Email, user.DisplayName, user.PasswordHash); err != nil {
		if isUniqueViolation(err) {
			return auth.User{}, auth.ErrEmailAlreadyRegistered
		}
		return auth.User{}, fmt.Errorf("insert user: %w", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_roles (user_id, role) VALUES ($1, $2)`, user.ID, user.Role); err != nil {
		return auth.User{}, fmt.Errorf("insert user role: %w", err)
	}
	return user.User, nil
}

func isUniqueViolation(err error) bool {
	var databaseError *pgconn.PgError
	return errors.As(err, &databaseError) && databaseError.Code == "23505"
}

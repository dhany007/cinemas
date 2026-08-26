package admin

import (
	"context"
	"errors"
)

var (
	// ErrInvalidCinemaInput indicates missing required cinema data.
	ErrInvalidCinemaInput = errors.New("invalid cinema input")
	// ErrCinemaNotFound indicates a cinema does not exist.
	ErrCinemaNotFound = errors.New("cinema not found")
)

// Cinema is an administrator-managed cinema location.
type Cinema struct {
	ID      string
	Name    string
	Address string
	City    string
}

// AuditEvent records a privileged administrative action.
type AuditEvent struct {
	ActorUserID string
	EntityType  string
	EntityID    string
	Action      string
}

// CreateCinemaInput contains data for an administrator-created cinema.
type CreateCinemaInput struct {
	ActorUserID string
	Name        string
	Address     string
	City        string
}

// UpdateCinemaInput contains the replacement data for a cinema.
type UpdateCinemaInput struct {
	ActorUserID string
	ID          string
	Name        string
	Address     string
	City        string
}

// Repository persists cinema data and its matching audit event atomically.
type Repository interface {
	CreateCinema(context.Context, Cinema, AuditEvent) (Cinema, error)
	ListCinemas(context.Context) ([]Cinema, error)
	FindCinema(context.Context, string) (Cinema, error)
	UpdateCinema(context.Context, Cinema, AuditEvent) (Cinema, error)
	DeleteCinema(context.Context, string, AuditEvent) error
}

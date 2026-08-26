package admin

import (
	"context"
	"errors"
)

// ErrInvalidCinemaInput indicates missing required cinema data.
var ErrInvalidCinemaInput = errors.New("invalid cinema input")

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

// Repository persists cinema data and its matching audit event atomically.
type Repository interface {
	CreateCinema(context.Context, Cinema, AuditEvent) (Cinema, error)
}

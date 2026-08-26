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
	// ErrStudioNotFound indicates a studio does not exist.
	ErrStudioNotFound = errors.New("studio not found")
)

// Cinema is an administrator-managed cinema location.
type Cinema struct {
	ID      string
	Name    string
	Address string
	City    string
}

// Studio is an administrator-managed auditorium in a cinema.
type Studio struct{ ID, CinemaID, Name string }

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

// CreateStudioInput contains data for an administrator-created studio.
type CreateStudioInput struct{ ActorUserID, CinemaID, Name string }

// UpdateStudioInput contains replacement data for a studio.
type UpdateStudioInput struct{ ActorUserID, ID, CinemaID, Name string }

// Repository persists cinema data and its matching audit event atomically.
type Repository interface {
	CreateCinema(context.Context, Cinema, AuditEvent) (Cinema, error)
	ListCinemas(context.Context) ([]Cinema, error)
	FindCinema(context.Context, string) (Cinema, error)
	UpdateCinema(context.Context, Cinema, AuditEvent) (Cinema, error)
	DeleteCinema(context.Context, string, AuditEvent) error
	CreateStudio(context.Context, Studio, AuditEvent) (Studio, error)
	ListStudios(context.Context) ([]Studio, error)
	UpdateStudio(context.Context, Studio, AuditEvent) (Studio, error)
	DeleteStudio(context.Context, string, AuditEvent) error
}

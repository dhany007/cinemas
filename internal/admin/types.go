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
	// ErrInvalidSeatInput indicates missing required seat-layout data.
	ErrInvalidSeatInput = errors.New("invalid seat input")
	// ErrSeatNotFound indicates a physical seat does not exist.
	ErrSeatNotFound = errors.New("seat not found")
	// ErrSeatAlreadyExists indicates a studio already has the row and seat number.
	ErrSeatAlreadyExists = errors.New("seat already exists")
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

// Seat is a physical seat in a studio's layout.
type Seat struct {
	ID         string
	StudioID   string
	RowLabel   string
	SeatNumber string
	SeatType   string
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

// CreateStudioInput contains data for an administrator-created studio.
type CreateStudioInput struct{ ActorUserID, CinemaID, Name string }

// UpdateStudioInput contains replacement data for a studio.
type UpdateStudioInput struct{ ActorUserID, ID, CinemaID, Name string }

// CreateSeatInput contains data for an administrator-created physical seat.
type CreateSeatInput struct {
	ActorUserID string
	StudioID    string
	RowLabel    string
	SeatNumber  string
	SeatType    string
}

// UpdateSeatInput contains replacement data for a physical seat.
type UpdateSeatInput struct {
	ActorUserID string
	ID          string
	StudioID    string
	RowLabel    string
	SeatNumber  string
	SeatType    string
}

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
	CreateSeat(context.Context, Seat, AuditEvent) (Seat, error)
	ListSeats(context.Context) ([]Seat, error)
	UpdateSeat(context.Context, Seat, AuditEvent) (Seat, error)
	DeleteSeat(context.Context, string, AuditEvent) error
}

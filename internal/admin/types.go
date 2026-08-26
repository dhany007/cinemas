package admin

import (
	"context"
	"errors"
	"time"
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
	// ErrInvalidMovieInput indicates invalid required or optional movie metadata.
	ErrInvalidMovieInput = errors.New("invalid movie input")
	// ErrMovieNotFound indicates a movie does not exist.
	ErrMovieNotFound = errors.New("movie not found")
	// ErrInvalidShowtimeInput indicates invalid showtime metadata.
	ErrInvalidShowtimeInput = errors.New("invalid showtime input")
	// ErrShowtimeNotFound indicates a showtime does not exist.
	ErrShowtimeNotFound = errors.New("showtime not found")
	// ErrShowtimeInUse indicates a showtime has dependent inventory or orders.
	ErrShowtimeInUse = errors.New("showtime in use")
	// ErrShowtimeOverlap indicates an interval overlaps another showtime in the same studio.
	ErrShowtimeOverlap = errors.New("showtime overlap")
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

// Movie is administrator-managed film metadata.
type Movie struct {
	ID              string
	Title           string
	DurationMinutes int
	Rating          *string
	Synopsis        *string
	PosterURL       *string
	ReleaseDate     *string
}

// Showtime is an administrator-managed screening and its pricing snapshot source.
type Showtime struct {
	ID        string
	MovieID   string
	StudioID  string
	StartsAt  time.Time
	EndsAt    time.Time
	BasePrice string
	Currency  string
}

// ShowtimeSeat is one materialized physical seat for a showtime.
type ShowtimeSeat struct {
	SeatID      string
	PriceAmount string
	Currency    string
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

// CreateMovieInput contains data for an administrator-created movie.
type CreateMovieInput struct {
	ActorUserID     string
	Title           string
	DurationMinutes int
	Rating          *string
	Synopsis        *string
	PosterURL       *string
	ReleaseDate     *string
}

// UpdateMovieInput contains replacement data for a movie.
type UpdateMovieInput struct {
	ActorUserID     string
	ID              string
	Title           string
	DurationMinutes int
	Rating          *string
	Synopsis        *string
	PosterURL       *string
	ReleaseDate     *string
}

// CreateShowtimeInput contains data for an administrator-created showtime.
type CreateShowtimeInput struct {
	ActorUserID string
	MovieID     string
	StudioID    string
	StartsAt    time.Time
	EndsAt      time.Time
	BasePrice   string
	Currency    string
}

// UpdateShowtimeInput contains replacement data for a showtime.
type UpdateShowtimeInput struct {
	ActorUserID string
	ID          string
	MovieID     string
	StudioID    string
	StartsAt    time.Time
	EndsAt      time.Time
	BasePrice   string
	Currency    string
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
	CreateMovie(context.Context, Movie, AuditEvent) (Movie, error)
	ListMovies(context.Context) ([]Movie, error)
	UpdateMovie(context.Context, Movie, AuditEvent) (Movie, error)
	DeleteMovie(context.Context, string, AuditEvent) error
	CreateShowtime(context.Context, Showtime, AuditEvent) (Showtime, error)
	ListShowtimes(context.Context) ([]Showtime, error)
	UpdateShowtime(context.Context, Showtime, AuditEvent) (Showtime, error)
	DeleteShowtime(context.Context, string, AuditEvent) error
}

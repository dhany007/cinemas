package admin

import (
	"context"
	"sort"
	"sync"
)

// MemoryRepository is a concurrency-safe repository used by tests.
type MemoryRepository struct {
	mu            sync.Mutex
	audits        []AuditEvent
	cinemas       map[string]Cinema
	studios       map[string]Studio
	seats         map[string]Seat
	movies        map[string]Movie
	showtimes     map[string]Showtime
	showtimeSeats map[string][]ShowtimeSeat
}

// NewMemoryRepository creates an empty test repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		cinemas:       make(map[string]Cinema),
		studios:       make(map[string]Studio),
		seats:         make(map[string]Seat),
		movies:        make(map[string]Movie),
		showtimes:     make(map[string]Showtime),
		showtimeSeats: make(map[string][]ShowtimeSeat),
	}
}

// CreateShowtime stores a showtime, materializes its seats, and records an audit event.
func (r *MemoryRepository) CreateShowtime(ctx context.Context, showtime Showtime, audit AuditEvent) (Showtime, error) {
	if err := ctx.Err(); err != nil {
		return Showtime{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.movies[showtime.MovieID]; !found {
		return Showtime{}, ErrMovieNotFound
	}
	if _, found := r.studios[showtime.StudioID]; !found {
		return Showtime{}, ErrStudioNotFound
	}
	if r.hasOverlappingShowtime(showtime) {
		return Showtime{}, ErrShowtimeOverlap
	}
	r.showtimes[showtime.ID] = showtime
	r.showtimeSeats[showtime.ID] = r.materializeShowtimeSeats(showtime)
	r.audits = append(r.audits, audit)
	return showtime, nil
}

// ListShowtimes returns stored showtimes in start-time and ID order.
func (r *MemoryRepository) ListShowtimes(ctx context.Context) ([]Showtime, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	showtimes := make([]Showtime, 0, len(r.showtimes))
	for _, showtime := range r.showtimes {
		showtimes = append(showtimes, showtime)
	}
	sort.Slice(showtimes, func(i, j int) bool {
		if showtimes[i].StartsAt.Equal(showtimes[j].StartsAt) {
			return showtimes[i].ID < showtimes[j].ID
		}
		return showtimes[i].StartsAt.Before(showtimes[j].StartsAt)
	})
	return showtimes, nil
}

// UpdateShowtime replaces a showtime, rematerializes its seats, and records an audit event.
func (r *MemoryRepository) UpdateShowtime(ctx context.Context, showtime Showtime, audit AuditEvent) (Showtime, error) {
	if err := ctx.Err(); err != nil {
		return Showtime{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.showtimes[showtime.ID]; !found {
		return Showtime{}, ErrShowtimeNotFound
	}
	if _, found := r.movies[showtime.MovieID]; !found {
		return Showtime{}, ErrMovieNotFound
	}
	if _, found := r.studios[showtime.StudioID]; !found {
		return Showtime{}, ErrStudioNotFound
	}
	if r.hasOverlappingShowtime(showtime) {
		return Showtime{}, ErrShowtimeOverlap
	}
	r.showtimes[showtime.ID] = showtime
	r.showtimeSeats[showtime.ID] = r.materializeShowtimeSeats(showtime)
	r.audits = append(r.audits, audit)
	return showtime, nil
}

// DeleteShowtime removes a showtime and its materialized seats, then records an audit event.
func (r *MemoryRepository) DeleteShowtime(ctx context.Context, id string, audit AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.showtimes[id]; !found {
		return ErrShowtimeNotFound
	}
	delete(r.showtimes, id)
	delete(r.showtimeSeats, id)
	r.audits = append(r.audits, audit)
	return nil
}

// ShowtimeSeats returns a test snapshot of materialized inventory.
func (r *MemoryRepository) ShowtimeSeats(showtimeID string) []ShowtimeSeat {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ShowtimeSeat(nil), r.showtimeSeats[showtimeID]...)
}

func (r *MemoryRepository) materializeShowtimeSeats(showtime Showtime) []ShowtimeSeat {
	seats := make([]ShowtimeSeat, 0)
	for _, seat := range r.seats {
		if seat.StudioID == showtime.StudioID {
			seats = append(seats, ShowtimeSeat{SeatID: seat.ID, PriceAmount: showtime.BasePrice, Currency: showtime.Currency})
		}
	}
	sort.Slice(seats, func(i, j int) bool { return seats[i].SeatID < seats[j].SeatID })
	return seats
}

func (r *MemoryRepository) hasOverlappingShowtime(candidate Showtime) bool {
	for _, existing := range r.showtimes {
		if existing.ID == candidate.ID || existing.StudioID != candidate.StudioID {
			continue
		}
		if candidate.StartsAt.Before(existing.EndsAt) && existing.StartsAt.Before(candidate.EndsAt) {
			return true
		}
	}
	return false
}

// CreateMovie stores a movie and its audit event.
func (r *MemoryRepository) CreateMovie(ctx context.Context, movie Movie, audit AuditEvent) (Movie, error) {
	if err := ctx.Err(); err != nil {
		return Movie{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.movies[movie.ID] = movie
	r.audits = append(r.audits, audit)
	return movie, nil
}

// ListMovies returns stored movies in deterministic title and ID order.
func (r *MemoryRepository) ListMovies(ctx context.Context) ([]Movie, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	movies := make([]Movie, 0, len(r.movies))
	for _, movie := range r.movies {
		movies = append(movies, movie)
	}
	sort.Slice(movies, func(i, j int) bool {
		if movies[i].Title == movies[j].Title {
			return movies[i].ID < movies[j].ID
		}
		return movies[i].Title < movies[j].Title
	})
	return movies, nil
}

// UpdateMovie replaces a movie and its audit event.
func (r *MemoryRepository) UpdateMovie(ctx context.Context, movie Movie, audit AuditEvent) (Movie, error) {
	if err := ctx.Err(); err != nil {
		return Movie{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.movies[movie.ID]; !found {
		return Movie{}, ErrMovieNotFound
	}
	r.movies[movie.ID] = movie
	r.audits = append(r.audits, audit)
	return movie, nil
}

// DeleteMovie removes a movie and records its audit event.
func (r *MemoryRepository) DeleteMovie(ctx context.Context, id string, audit AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.movies[id]; !found {
		return ErrMovieNotFound
	}
	delete(r.movies, id)
	r.audits = append(r.audits, audit)
	return nil
}

// CreateSeat stores a physical seat and its audit event.
func (r *MemoryRepository) CreateSeat(ctx context.Context, seat Seat, audit AuditEvent) (Seat, error) {
	if err := ctx.Err(); err != nil {
		return Seat{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.studios[seat.StudioID]; !found {
		return Seat{}, ErrStudioNotFound
	}
	if r.hasSeatLayoutPosition(seat) {
		return Seat{}, ErrSeatAlreadyExists
	}
	r.seats[seat.ID] = seat
	r.audits = append(r.audits, audit)
	return seat, nil
}

// ListSeats returns stored seats in stable layout order.
func (r *MemoryRepository) ListSeats(ctx context.Context) ([]Seat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	seats := make([]Seat, 0, len(r.seats))
	for _, seat := range r.seats {
		seats = append(seats, seat)
	}
	sort.Slice(seats, func(i, j int) bool {
		if seats[i].StudioID != seats[j].StudioID {
			return seats[i].StudioID < seats[j].StudioID
		}
		if seats[i].RowLabel != seats[j].RowLabel {
			return seats[i].RowLabel < seats[j].RowLabel
		}
		if seats[i].SeatNumber != seats[j].SeatNumber {
			return seats[i].SeatNumber < seats[j].SeatNumber
		}
		return seats[i].ID < seats[j].ID
	})
	return seats, nil
}

// UpdateSeat replaces a physical seat and its audit event.
func (r *MemoryRepository) UpdateSeat(ctx context.Context, seat Seat, audit AuditEvent) (Seat, error) {
	if err := ctx.Err(); err != nil {
		return Seat{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.seats[seat.ID]; !found {
		return Seat{}, ErrSeatNotFound
	}
	if _, found := r.studios[seat.StudioID]; !found {
		return Seat{}, ErrStudioNotFound
	}
	if r.hasSeatLayoutPosition(seat) {
		return Seat{}, ErrSeatAlreadyExists
	}
	r.seats[seat.ID] = seat
	r.audits = append(r.audits, audit)
	return seat, nil
}

// DeleteSeat removes a physical seat and records its audit event.
func (r *MemoryRepository) DeleteSeat(ctx context.Context, id string, audit AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, found := r.seats[id]; !found {
		return ErrSeatNotFound
	}
	delete(r.seats, id)
	r.audits = append(r.audits, audit)
	return nil
}

func (r *MemoryRepository) hasSeatLayoutPosition(candidate Seat) bool {
	for _, seat := range r.seats {
		if seat.ID != candidate.ID && seat.StudioID == candidate.StudioID && seat.RowLabel == candidate.RowLabel &&
			seat.SeatNumber == candidate.SeatNumber {
			return true
		}
	}
	return false
}

// CreateStudio stores a studio and audit event.
func (r *MemoryRepository) CreateStudio(_ context.Context, s Studio, a AuditEvent) (Studio, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.cinemas[s.CinemaID]; !ok {
		return Studio{}, ErrCinemaNotFound
	}
	r.studios[s.ID] = s
	r.audits = append(r.audits, a)
	return s, nil
}

// ListStudios returns stored studios.
func (r *MemoryRepository) ListStudios(context.Context) ([]Studio, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Studio, 0, len(r.studios))
	for _, s := range r.studios {
		result = append(result, s)
	}
	return result, nil
}

// UpdateStudio replaces a stored studio and adds an audit event.
func (r *MemoryRepository) UpdateStudio(_ context.Context, s Studio, a AuditEvent) (Studio, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.studios[s.ID]; !ok {
		return Studio{}, ErrStudioNotFound
	}
	if _, ok := r.cinemas[s.CinemaID]; !ok {
		return Studio{}, ErrCinemaNotFound
	}
	r.studios[s.ID] = s
	r.audits = append(r.audits, a)
	return s, nil
}

// DeleteStudio removes a stored studio and adds an audit event.
func (r *MemoryRepository) DeleteStudio(_ context.Context, id string, a AuditEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.studios[id]; !ok {
		return ErrStudioNotFound
	}
	delete(r.studios, id)
	r.audits = append(r.audits, a)
	return nil
}

// CreateCinema stores a cinema and audit event together.
func (r *MemoryRepository) CreateCinema(ctx context.Context, cinema Cinema, audit AuditEvent) (Cinema, error) {
	if err := ctx.Err(); err != nil {
		return Cinema{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cinemas[cinema.ID] = cinema
	r.audits = append(r.audits, audit)
	return cinema, nil
}

// ListCinemas returns cinemas in the same stable order as the PostgreSQL repository.
func (r *MemoryRepository) ListCinemas(ctx context.Context) ([]Cinema, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	cinemas := make([]Cinema, 0, len(r.cinemas))
	for _, cinema := range r.cinemas {
		cinemas = append(cinemas, cinema)
	}
	sort.Slice(cinemas, func(i, j int) bool {
		if cinemas[i].Name == cinemas[j].Name {
			return cinemas[i].ID < cinemas[j].ID
		}
		return cinemas[i].Name < cinemas[j].Name
	})
	return cinemas, nil
}

// FindCinema returns a cinema by ID.
func (r *MemoryRepository) FindCinema(ctx context.Context, id string) (Cinema, error) {
	if err := ctx.Err(); err != nil {
		return Cinema{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	cinema, found := r.cinemas[id]
	if !found {
		return Cinema{}, ErrCinemaNotFound
	}
	return cinema, nil
}

// UpdateCinema stores a cinema replacement and matching audit event together.
func (r *MemoryRepository) UpdateCinema(ctx context.Context, cinema Cinema, audit AuditEvent) (Cinema, error) {
	if err := ctx.Err(); err != nil {
		return Cinema{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, found := r.cinemas[cinema.ID]; !found {
		return Cinema{}, ErrCinemaNotFound
	}
	r.cinemas[cinema.ID] = cinema
	r.audits = append(r.audits, audit)
	return cinema, nil
}

// DeleteCinema removes a cinema and records the matching audit event together.
func (r *MemoryRepository) DeleteCinema(ctx context.Context, id string, audit AuditEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, found := r.cinemas[id]; !found {
		return ErrCinemaNotFound
	}
	delete(r.cinemas, id)
	r.audits = append(r.audits, audit)
	return nil
}

// AuditEvents returns a test snapshot of recorded audit events.
func (r *MemoryRepository) AuditEvents() []AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AuditEvent(nil), r.audits...)
}

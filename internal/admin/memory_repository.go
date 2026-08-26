package admin

import (
	"context"
	"sort"
	"sync"
)

// MemoryRepository is a concurrency-safe repository used by tests.
type MemoryRepository struct {
	mu      sync.Mutex
	audits  []AuditEvent
	cinemas map[string]Cinema
	studios map[string]Studio
}

// NewMemoryRepository creates an empty test repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{cinemas: make(map[string]Cinema), studios: make(map[string]Studio)}
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

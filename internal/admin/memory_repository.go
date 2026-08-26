package admin

import (
	"context"
	"sync"
)

// MemoryRepository is a concurrency-safe repository used by tests.
type MemoryRepository struct {
	mu     sync.Mutex
	audits []AuditEvent
}

// NewMemoryRepository creates an empty test repository.
func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{} }

// CreateCinema stores a cinema and audit event together.
func (r *MemoryRepository) CreateCinema(ctx context.Context, cinema Cinema, audit AuditEvent) (Cinema, error) {
	if err := ctx.Err(); err != nil {
		return Cinema{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audits = append(r.audits, audit)
	return cinema, nil
}

// AuditEvents returns a test snapshot of recorded audit events.
func (r *MemoryRepository) AuditEvents() []AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AuditEvent(nil), r.audits...)
}

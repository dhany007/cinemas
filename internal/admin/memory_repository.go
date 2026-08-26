package admin

import (
	"context"
	"sync"
)

type MemoryRepository struct {
	mu     sync.Mutex
	audits []AuditEvent
}

func NewMemoryRepository() *MemoryRepository { return &MemoryRepository{} }

func (r *MemoryRepository) CreateCinema(ctx context.Context, cinema Cinema, audit AuditEvent) (Cinema, error) {
	if err := ctx.Err(); err != nil {
		return Cinema{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.audits = append(r.audits, audit)
	return cinema, nil
}

func (r *MemoryRepository) AuditEvents() []AuditEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]AuditEvent(nil), r.audits...)
}

package auth

import (
	"context"
	"sync"
)

// MemoryRepository is a concurrency-safe authentication repository for tests.
type MemoryRepository struct {
	mu           sync.RWMutex
	usersByEmail map[string]StoredUser
	adminExists  bool
}

// NewMemoryRepository creates an empty in-memory authentication repository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{usersByEmail: make(map[string]StoredUser)}
}

// CreateUser stores a customer account.
func (r *MemoryRepository) CreateUser(ctx context.Context, user NewUser) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createUser(user)
}

// CreateInitialAdmin stores the first administrator account only.
func (r *MemoryRepository) CreateInitialAdmin(ctx context.Context, user NewUser) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.adminExists {
		return User{}, ErrAdminAlreadyBootstrapped
	}
	created, err := r.createUser(user)
	if err != nil {
		return User{}, err
	}
	r.adminExists = true
	return created, nil
}

// FindUserByEmail returns a stored user for login validation.
func (r *MemoryRepository) FindUserByEmail(ctx context.Context, email string) (StoredUser, error) {
	if err := ctx.Err(); err != nil {
		return StoredUser{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	user, found := r.usersByEmail[email]
	if !found {
		return StoredUser{}, ErrInvalidCredentials
	}
	return user, nil
}

func (r *MemoryRepository) createUser(user NewUser) (User, error) {
	if _, found := r.usersByEmail[user.Email]; found {
		return User{}, ErrEmailAlreadyRegistered
	}
	r.usersByEmail[user.Email] = StoredUser(user)
	return user.User, nil
}

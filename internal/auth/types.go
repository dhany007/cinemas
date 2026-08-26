package auth

import (
	"context"
	"errors"
	"time"
)

const (
	// RoleCustomer is the role assigned to self-registered cinema customers.
	RoleCustomer Role = "CUSTOMER"
	// RoleAdmin is the role assigned through the protected bootstrap endpoint.
	RoleAdmin Role = "ADMIN"
)

var (
	// ErrInvalidInput indicates malformed registration or login input.
	ErrInvalidInput = errors.New("invalid authentication input")
	// ErrEmailAlreadyRegistered indicates a duplicate email address.
	ErrEmailAlreadyRegistered = errors.New("email already registered")
	// ErrInvalidCredentials intentionally does not distinguish unknown email from bad password.
	ErrInvalidCredentials = errors.New("invalid credentials")
	// ErrInvalidToken indicates an untrusted, malformed, or expired access token.
	ErrInvalidToken = errors.New("invalid access token")
	// ErrAdminAlreadyBootstrapped indicates that the one-time admin bootstrap has completed.
	ErrAdminAlreadyBootstrapped = errors.New("admin already bootstrapped")
)

// Role controls API permissions.
type Role string

// User is the non-sensitive user profile returned by authentication operations.
type User struct {
	ID          string
	Email       string
	DisplayName string
	Role        Role
}

// Identity is the authenticated request principal.
type Identity struct {
	UserID string
	Role   Role
}

// RegisterInput is the input accepted for customer and initial-admin registration.
type RegisterInput struct {
	Email       string
	Password    string
	DisplayName string
}

// LoginInput is the input accepted for password login.
type LoginInput struct {
	Email    string
	Password string
}

// Session is the authentication result exposed to API callers.
type Session struct {
	AccessToken string
	User        User
}

// StoredUser includes the password hash required only within authentication persistence.
type StoredUser struct {
	User
	PasswordHash string
}

// NewUser is the persistence input for a new user.
type NewUser struct {
	User
	PasswordHash string
}

// Repository persists authentication identities.
type Repository interface {
	CreateUser(ctx context.Context, user NewUser) (User, error)
	CreateInitialAdmin(ctx context.Context, user NewUser) (User, error)
	FindUserByEmail(ctx context.Context, email string) (StoredUser, error)
}

// Clock is kept as a named type to make token expiry deterministic in tests.
type Clock func() time.Time

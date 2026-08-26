package auth

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceRegisterThenLogin(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	service := NewService(NewMemoryRepository(), []byte("01234567890123456789012345678901"), time.Hour, func() time.Time {
		return now
	})

	registered, err := service.Register(context.Background(), RegisterInput{
		Email:       "customer@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Customer",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if registered.User.Role != RoleCustomer {
		t.Fatalf("role = %q, want %q", registered.User.Role, RoleCustomer)
	}

	identity, err := service.Authenticate(context.Background(), registered.AccessToken)
	if err != nil {
		t.Fatalf("Authenticate() error = %v", err)
	}
	if identity.UserID != registered.User.ID || identity.Role != RoleCustomer {
		t.Fatalf("identity = %#v, want registered customer", identity)
	}

	loggedIn, err := service.Login(context.Background(), LoginInput{
		Email:    "customer@example.com",
		Password: "correct horse battery staple",
	})
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if loggedIn.User.ID != registered.User.ID {
		t.Fatalf("logged-in user ID = %q, want %q", loggedIn.User.ID, registered.User.ID)
	}
}

func TestServiceLoginRejectsIncorrectPassword(t *testing.T) {
	service := NewService(NewMemoryRepository(), []byte("01234567890123456789012345678901"), time.Hour, time.Now)
	_, err := service.Register(context.Background(), RegisterInput{
		Email:       "customer@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Customer",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	_, err = service.Login(context.Background(), LoginInput{
		Email:    "customer@example.com",
		Password: "wrong password value",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
	}
}

func TestServiceRegisterAdminCreatesAdminOnly(t *testing.T) {
	service := NewService(NewMemoryRepository(), []byte("01234567890123456789012345678901"), time.Hour, time.Now)

	admin, err := service.RegisterAdmin(context.Background(), RegisterInput{
		Email:       "admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Admin",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	if admin.User.Role != RoleAdmin {
		t.Fatalf("role = %q, want %q", admin.User.Role, RoleAdmin)
	}

	_, err = service.RegisterAdmin(context.Background(), RegisterInput{
		Email:       "second-admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Second Admin",
	})
	if !errors.Is(err, ErrAdminAlreadyBootstrapped) {
		t.Fatalf("RegisterAdmin() error = %v, want ErrAdminAlreadyBootstrapped", err)
	}
}

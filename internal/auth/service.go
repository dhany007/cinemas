package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const (
	minimumPasswordLength = 12
	maximumEmailLength    = 254
	maximumDisplayNameLen = 100
	bcryptCost            = bcrypt.DefaultCost
	uuidByteLength        = 16
	uuidVersionIndex      = 6
	uuidVariantIndex      = 8
	uuidVersionMask       = 0x0f
	uuidVersion4          = 0x40
	uuidVariantMask       = 0x3f
	uuidVariantRFC        = 0x80
)

// Service applies registration, password login, and token verification rules.
type Service struct {
	repository Repository
	signer     tokenSigner
	clock      Clock
	newID      func() (string, error)
}

// NewService creates an authentication service using the supplied access-token secret.
func NewService(repository Repository, secret []byte, tokenTTL time.Duration, clock Clock) *Service {
	return &Service{
		repository: repository,
		signer: tokenSigner{
			secret: secret,
			ttl:    tokenTTL,
		},
		clock: clock,
		newID: randomID,
	}
}

// Register creates a customer account and returns an access token.
func (s *Service) Register(ctx context.Context, input RegisterInput) (Session, error) {
	return s.register(ctx, input, RoleCustomer, s.repository.CreateUser)
}

// RegisterAdmin creates the initial administrator account.
func (s *Service) RegisterAdmin(ctx context.Context, input RegisterInput) (Session, error) {
	return s.register(ctx, input, RoleAdmin, s.repository.CreateInitialAdmin)
}

// Login verifies an email/password pair and returns a new access token.
func (s *Service) Login(ctx context.Context, input LoginInput) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	email := normalizeEmail(input.Email)
	if email == "" || strings.TrimSpace(input.Password) == "" {
		return Session{}, ErrInvalidInput
	}

	storedUser, err := s.repository.FindUserByEmail(ctx, email)
	if err != nil {
		if errorsIsInvalidCredentials(err) {
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, fmt.Errorf("find user by email: %w", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(storedUser.PasswordHash), []byte(input.Password)) != nil {
		return Session{}, ErrInvalidCredentials
	}
	return s.newSession(storedUser.User)
}

// Authenticate parses and verifies an access token.
func (s *Service) Authenticate(ctx context.Context, accessToken string) (Identity, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}
	return s.signer.verify(accessToken, s.clock().UTC())
}

func (s *Service) register(
	ctx context.Context,
	input RegisterInput,
	role Role,
	create func(context.Context, NewUser) (User, error),
) (Session, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, err
	}
	email, displayName, err := validateRegistrationInput(input)
	if err != nil {
		return Session{}, err
	}
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcryptCost)
	if err != nil {
		return Session{}, fmt.Errorf("hash password: %w", err)
	}
	id, err := s.newID()
	if err != nil {
		return Session{}, fmt.Errorf("generate user id: %w", err)
	}
	user, err := create(ctx, NewUser{User: User{
		ID:          id,
		Email:       email,
		DisplayName: displayName,
		Role:        role,
	}, PasswordHash: string(passwordHash)})
	if err != nil {
		return Session{}, fmt.Errorf("create user: %w", err)
	}
	return s.newSession(user)
}

func (s *Service) newSession(user User) (Session, error) {
	accessToken, err := s.signer.sign(Identity{UserID: user.ID, Role: user.Role}, s.clock().UTC())
	if err != nil {
		return Session{}, fmt.Errorf("sign access token: %w", err)
	}
	return Session{AccessToken: accessToken, User: user}, nil
}

func validateRegistrationInput(input RegisterInput) (string, string, error) {
	email := normalizeEmail(input.Email)
	displayName := strings.TrimSpace(input.DisplayName)
	if email == "" || len(email) > maximumEmailLength || len(input.Password) < minimumPasswordLength ||
		displayName == "" || len(displayName) > maximumDisplayNameLen {
		return "", "", ErrInvalidInput
	}
	return email, displayName, nil
}

func normalizeEmail(value string) string {
	email := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || !strings.Contains(email, "@") {
		return ""
	}
	return email
}

func errorsIsInvalidCredentials(err error) bool {
	return errors.Is(err, ErrInvalidCredentials)
}

func randomID() (string, error) {
	bytes := make([]byte, uuidByteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[uuidVersionIndex] = bytes[uuidVersionIndex]&uuidVersionMask | uuidVersion4
	bytes[uuidVariantIndex] = bytes[uuidVariantIndex]&uuidVariantMask | uuidVariantRFC
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]),
		hex.EncodeToString(bytes[4:6]),
		hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]),
		hex.EncodeToString(bytes[10:16]),
	), nil
}

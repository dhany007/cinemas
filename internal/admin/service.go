package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	uuidByteLength   = 16
	uuidVersionIndex = 6
	uuidVariantIndex = 8
	uuidVersionMask  = 0x0f
	uuidVersion4     = 0x40
	uuidVariantMask  = 0x3f
	uuidVariantRFC   = 0x80
)

// Service applies administrator cinema management rules.
type Service struct {
	repository Repository
	newID      func() (string, error)
}

// NewService creates an administrator cinema service.
func NewService(repository Repository) *Service {
	return &Service{repository: repository, newID: newCinemaID}
}

// CreateCinema validates, creates, and audits a cinema.
func (s *Service) CreateCinema(ctx context.Context, input CreateCinemaInput) (Cinema, error) {
	if strings.TrimSpace(input.ActorUserID) == "" || strings.TrimSpace(input.Name) == "" ||
		strings.TrimSpace(input.Address) == "" || strings.TrimSpace(input.City) == "" {
		return Cinema{}, ErrInvalidCinemaInput
	}
	id, err := s.newID()
	if err != nil {
		return Cinema{}, fmt.Errorf("generate cinema id: %w", err)
	}
	cinema := Cinema{ID: id, Name: strings.TrimSpace(input.Name), Address: strings.TrimSpace(input.Address),
		City: strings.TrimSpace(input.City)}
	audit := AuditEvent{ActorUserID: input.ActorUserID, EntityType: "CINEMA", EntityID: id, Action: "CREATE"}
	return s.repository.CreateCinema(ctx, cinema, audit)
}

func newCinemaID() (string, error) {
	bytes := make([]byte, uuidByteLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[uuidVersionIndex] = bytes[uuidVersionIndex]&uuidVersionMask | uuidVersion4
	bytes[uuidVariantIndex] = bytes[uuidVariantIndex]&uuidVariantMask | uuidVariantRFC
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]), hex.EncodeToString(bytes[4:6]), hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]), hex.EncodeToString(bytes[10:16]),
	), nil
}

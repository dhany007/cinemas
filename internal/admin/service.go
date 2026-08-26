package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

type Service struct {
	repository Repository
	newID      func() (string, error)
}

func NewService(repository Repository) *Service {
	return &Service{repository: repository, newID: newCinemaID}
}

func (s *Service) CreateCinema(ctx context.Context, input CreateCinemaInput) (Cinema, error) {
	if strings.TrimSpace(input.ActorUserID) == "" || strings.TrimSpace(input.Name) == "" || strings.TrimSpace(input.Address) == "" || strings.TrimSpace(input.City) == "" {
		return Cinema{}, ErrInvalidCinemaInput
	}
	id, err := s.newID()
	if err != nil {
		return Cinema{}, fmt.Errorf("generate cinema id: %w", err)
	}
	cinema := Cinema{ID: id, Name: strings.TrimSpace(input.Name), Address: strings.TrimSpace(input.Address), City: strings.TrimSpace(input.City)}
	return s.repository.CreateCinema(ctx, cinema, AuditEvent{ActorUserID: input.ActorUserID, EntityType: "CINEMA", EntityID: id, Action: "CREATE"})
}

func newCinemaID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = bytes[6]&0x0f | 0x40
	bytes[8] = bytes[8]&0x3f | 0x80
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		hex.EncodeToString(bytes[0:4]), hex.EncodeToString(bytes[4:6]), hex.EncodeToString(bytes[6:8]),
		hex.EncodeToString(bytes[8:10]), hex.EncodeToString(bytes[10:16]),
	), nil
}

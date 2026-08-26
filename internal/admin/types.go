package admin

import (
	"context"
	"errors"
)

var ErrInvalidCinemaInput = errors.New("invalid cinema input")

type Cinema struct {
	ID      string
	Name    string
	Address string
	City    string
}

type AuditEvent struct {
	ActorUserID string
	EntityType  string
	EntityID    string
	Action      string
}

type CreateCinemaInput struct {
	ActorUserID string
	Name        string
	Address     string
	City        string
}

type Repository interface {
	CreateCinema(context.Context, Cinema, AuditEvent) (Cinema, error)
}

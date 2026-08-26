package admin

import (
	"context"
	"testing"
)

func TestServiceCreatesCinemaAndRecordsAuditEvent(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)

	cinema, err := service.CreateCinema(context.Background(), CreateCinemaInput{
		ActorUserID: "10000000-0000-4000-8000-000000000001",
		Name:        "Central Cinema",
		Address:     "Jl. Example 1",
		City:        "Jakarta",
	})
	if err != nil {
		t.Fatalf("CreateCinema() error = %v", err)
	}
	if cinema.Name != "Central Cinema" || cinema.City != "Jakarta" {
		t.Fatalf("cinema = %#v, want created cinema", cinema)
	}
	if got := repository.AuditEvents(); len(got) != 1 || got[0].ActorUserID != "10000000-0000-4000-8000-000000000001" {
		t.Fatalf("audit events = %#v, want one actor event", got)
	}
}

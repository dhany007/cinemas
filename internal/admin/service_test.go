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

func TestServiceManagesSeatLayoutAndRecordsAuditEvents(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	ctx := context.Background()
	actorUserID := "10000000-0000-4000-8000-000000000001"

	cinema, err := service.CreateCinema(ctx, CreateCinemaInput{
		ActorUserID: actorUserID,
		Name:        "Central Cinema",
		Address:     "Jl. Example 1",
		City:        "Jakarta",
	})
	if err != nil {
		t.Fatalf("CreateCinema() error = %v", err)
	}
	studio, err := service.CreateStudio(ctx, CreateStudioInput{
		ActorUserID: actorUserID,
		CinemaID:    cinema.ID,
		Name:        "Studio 1",
	})
	if err != nil {
		t.Fatalf("CreateStudio() error = %v", err)
	}

	seat, err := service.CreateSeat(ctx, CreateSeatInput{
		ActorUserID: actorUserID,
		StudioID:    studio.ID,
		RowLabel:    "A",
		SeatNumber:  "1",
		SeatType:    "STANDARD",
	})
	if err != nil {
		t.Fatalf("CreateSeat() error = %v", err)
	}
	if seat.StudioID != studio.ID || seat.RowLabel != "A" || seat.SeatNumber != "1" || seat.SeatType != "STANDARD" {
		t.Fatalf("seat = %#v, want created layout seat", seat)
	}

	seats, err := service.ListSeats(ctx)
	if err != nil {
		t.Fatalf("ListSeats() error = %v", err)
	}
	if len(seats) != 1 || seats[0] != seat {
		t.Fatalf("ListSeats() = %#v, want %#v", seats, []Seat{seat})
	}

	updated, err := service.UpdateSeat(ctx, UpdateSeatInput{
		ActorUserID: actorUserID,
		ID:          seat.ID,
		StudioID:    studio.ID,
		RowLabel:    "A",
		SeatNumber:  "2",
		SeatType:    "PREMIUM",
	})
	if err != nil {
		t.Fatalf("UpdateSeat() error = %v", err)
	}
	if updated.SeatNumber != "2" || updated.SeatType != "PREMIUM" {
		t.Fatalf("updated seat = %#v, want replacement values", updated)
	}

	if err := service.DeleteSeat(ctx, actorUserID, seat.ID); err != nil {
		t.Fatalf("DeleteSeat() error = %v", err)
	}
	if got := repository.AuditEvents(); len(got) != 5 || got[2].EntityType != "SEAT" || got[4].Action != "DELETE" {
		t.Fatalf("audit events = %#v, want cinema, studio, and seat mutations", got)
	}
}

func TestServiceManagesMovieAndRecordsAuditEvents(t *testing.T) {
	repository := NewMemoryRepository()
	service := NewService(repository)
	ctx := context.Background()
	actorUserID := "10000000-0000-4000-8000-000000000001"
	rating := "PG-13"
	synopsis := "A thrilling adventure."
	posterURL := "https://example.com/poster.jpg"
	releaseDate := "2026-08-26"

	movie, err := service.CreateMovie(ctx, CreateMovieInput{
		ActorUserID:     actorUserID,
		Title:           "Example Movie",
		DurationMinutes: 120,
		Rating:          &rating,
		Synopsis:        &synopsis,
		PosterURL:       &posterURL,
		ReleaseDate:     &releaseDate,
	})
	if err != nil {
		t.Fatalf("CreateMovie() error = %v", err)
	}
	if movie.Title != "Example Movie" || movie.DurationMinutes != 120 ||
		movie.ReleaseDate == nil || *movie.ReleaseDate != releaseDate {
		t.Fatalf("movie = %#v, want created movie", movie)
	}

	movies, err := service.ListMovies(ctx)
	if err != nil {
		t.Fatalf("ListMovies() error = %v", err)
	}
	if len(movies) != 1 || movies[0] != movie {
		t.Fatalf("ListMovies() = %#v, want %#v", movies, []Movie{movie})
	}

	updated, err := service.UpdateMovie(ctx, UpdateMovieInput{
		ActorUserID:     actorUserID,
		ID:              movie.ID,
		Title:           "Example Movie: Director's Cut",
		DurationMinutes: 130,
		Rating:          &rating,
		Synopsis:        &synopsis,
		PosterURL:       &posterURL,
		ReleaseDate:     &releaseDate,
	})
	if err != nil {
		t.Fatalf("UpdateMovie() error = %v", err)
	}
	if updated.Title != "Example Movie: Director's Cut" || updated.DurationMinutes != 130 {
		t.Fatalf("updated movie = %#v, want replacement values", updated)
	}

	if err := service.DeleteMovie(ctx, actorUserID, movie.ID); err != nil {
		t.Fatalf("DeleteMovie() error = %v", err)
	}
	if got := repository.AuditEvents(); len(got) != 3 || got[0].EntityType != "MOVIE" || got[2].Action != "DELETE" {
		t.Fatalf("audit events = %#v, want movie mutations", got)
	}
}

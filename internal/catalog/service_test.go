package catalog

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceListMoviesReturnsNextCursor(t *testing.T) {
	movies := []Movie{
		{
			ID:              "10000000-0000-4000-8000-000000000001",
			Title:           "First Movie",
			DurationMinutes: 120,
			CreatedAt:       time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC),
		},
		{
			ID:              "10000000-0000-4000-8000-000000000002",
			Title:           "Second Movie",
			DurationMinutes: 90,
			CreatedAt:       time.Date(2026, time.August, 26, 9, 0, 0, 0, time.UTC),
		},
	}
	service := NewService(NewMemoryRepository(movies))

	page, err := service.ListMovies(context.Background(), ListInput{Limit: 1})
	if err != nil {
		t.Fatalf("ListMovies() error = %v", err)
	}
	if len(page.Movies) != 1 {
		t.Fatalf("movie count = %d, want 1", len(page.Movies))
	}
	if page.Movies[0].Title != "First Movie" {
		t.Fatalf("movie title = %q, want First Movie", page.Movies[0].Title)
	}
	if page.NextCursor == "" {
		t.Fatal("next cursor is empty")
	}

	nextPage, err := service.ListMovies(context.Background(), ListInput{
		Limit:  1,
		Cursor: page.NextCursor,
	})
	if err != nil {
		t.Fatalf("ListMovies() next page error = %v", err)
	}
	if len(nextPage.Movies) != 1 || nextPage.Movies[0].Title != "Second Movie" {
		t.Fatalf("next page movies = %#v, want Second Movie", nextPage.Movies)
	}
}

func TestServiceListMoviesRejectsInvalidCursor(t *testing.T) {
	service := NewService(NewMemoryRepository(nil))

	_, err := service.ListMovies(context.Background(), ListInput{Limit: 1, Cursor: "invalid"})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("ListMovies() error = %v, want ErrInvalidCursor", err)
	}
}

func TestParsePageSize(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int
		wantErr bool
	}{
		{name: "default", want: DefaultPageSize},
		{name: "valid", value: "10", want: 10},
		{name: "zero", value: "0", wantErr: true},
		{name: "too large", value: "101", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ParsePageSize(test.value)
			if test.wantErr {
				if !errors.Is(err, ErrInvalidPageSize) {
					t.Fatalf("ParsePageSize() error = %v, want ErrInvalidPageSize", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParsePageSize() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("page size = %d, want %d", got, test.want)
			}
		})
	}
}

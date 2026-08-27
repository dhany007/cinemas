package seed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRunnerSeedsCinemaStudiosAndOMDbMoviesIdempotently(t *testing.T) {
	t.Helper()
	state := newFakeAPIState()
	api := httptest.NewServer(state.handler(t))
	t.Cleanup(api.Close)
	omdbCalls := 0
	omdb := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		omdbCalls++
		if request.URL.Query().Get("apikey") != "test-key" {
			t.Fatal("OMDb request did not include API key")
		}
		if request.URL.Query().Get("type") != "movie" {
			t.Fatal("OMDb request did not request a movie")
		}
		_ = json.NewEncoder(writer).Encode(map[string]string{
			"Response": "True", "Title": request.URL.Query().Get("t"), "Runtime": "120 min",
			"Rated": "PG-13", "Plot": "A test film.", "Poster": "https://images.example.test/poster.jpg", "Released": "16 Jul 2010",
		})
	}))
	t.Cleanup(omdb.Close)

	runner := NewRunner(api.Client(), []Cinema{{Name: "Central Cinema", Address: "Jl. Example 1", City: "Jakarta", Studios: []string{"Studio 1", "Studio 2"}}}, []string{"Example Film"})
	config := Config{APIBaseURL: api.URL, OMDbBaseURL: omdb.URL, OMDbAPIKey: "test-key", AdminBootstrapToken: "bootstrap", AdminEmail: "admin@example.test", AdminPassword: "correct horse battery staple", AdminDisplayName: "Local Admin"}
	if err := runner.Run(context.Background(), config); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if err := runner.Run(context.Background(), config); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	if got := len(state.cinemas); got != 1 {
		t.Fatalf("cinemas = %d, want 1", got)
	}
	if got := len(state.studios); got != 2 {
		t.Fatalf("studios = %d, want 2", got)
	}
	if got := len(state.movies); got != 1 {
		t.Fatalf("movies = %d, want 1", got)
	}
	if got := omdbCalls; got != 1 {
		t.Fatalf("OMDb calls = %d, want 1 because existing movies are not fetched again", got)
	}
	if state.movies[0].DurationMinutes != 120 || state.movies[0].ReleaseDate != "2010-07-16" {
		t.Fatalf("movie = %#v, want OMDb runtime and ISO release date", state.movies[0])
	}
}

func TestConfigValidationRejectsMissingOMDbKey(t *testing.T) {
	config := Config{APIBaseURL: "http://api.example.test", OMDbBaseURL: "https://www.omdbapi.com/", AdminBootstrapToken: "bootstrap", AdminEmail: "admin@example.test", AdminPassword: "correct horse battery staple", AdminDisplayName: "Local Admin"}
	if err := config.Validate(); err == nil || !strings.Contains(err.Error(), "OMDB_API_KEY") {
		t.Fatalf("Validate() error = %v, want missing OMDB_API_KEY error", err)
	}
}

func TestRunnerDoesNotWriteCatalogWhenOMDbPreflightFails(t *testing.T) {
	state := newFakeAPIState()
	api := httptest.NewServer(state.handler(t))
	t.Cleanup(api.Close)
	omdb := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(omdb.Close)

	runner := NewRunner(api.Client(), []Cinema{{Name: "Central Cinema", Address: "Jl. Example 1", City: "Jakarta", Studios: []string{"Studio 1"}}}, []string{"Example Film"})
	config := Config{APIBaseURL: api.URL, OMDbBaseURL: omdb.URL, OMDbAPIKey: "test-key", AdminBootstrapToken: "bootstrap", AdminEmail: "admin@example.test", AdminPassword: "correct horse battery staple", AdminDisplayName: "Local Admin"}
	if err := runner.Run(context.Background(), config); err == nil {
		t.Fatal("Run() error = nil, want OMDb preflight failure")
	}
	if len(state.cinemas) != 0 || len(state.studios) != 0 || len(state.movies) != 0 {
		t.Fatalf("catalog was written after OMDb failure: cinemas=%d studios=%d movies=%d", len(state.cinemas), len(state.studios), len(state.movies))
	}
}

type fakeAPIState struct {
	cinemas []Cinema
	studios []fakeStudio
	movies  []fakeMovie
}

type fakeStudio struct {
	ID       string `json:"id"`
	CinemaID string `json:"cinema_id"`
	Name     string `json:"name"`
}
type fakeMovie struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	DurationMinutes int    `json:"duration_minutes"`
	ReleaseDate     string `json:"release_date"`
}

func newFakeAPIState() *fakeAPIState { return &fakeAPIState{} }

func (state *fakeAPIState) handler(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/v1/auth/bootstrap-admin" {
			writer.WriteHeader(http.StatusConflict)
			return
		}
		if request.URL.Path == "/v1/auth/login" {
			_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "token", "user": map[string]string{"role": "ADMIN"}})
			return
		}
		if request.Header.Get("Authorization") != "Bearer token" {
			t.Fatal("admin request did not include bearer token")
		}
		switch request.URL.Path {
		case "/v1/admin/cinemas":
			if request.Method == http.MethodGet {
				_ = json.NewEncoder(writer).Encode(map[string]any{"cinemas": state.cinemas})
				return
			}
			var cinema Cinema
			_ = json.NewDecoder(request.Body).Decode(&cinema)
			cinema.ID = "cinema-1"
			state.cinemas = append(state.cinemas, cinema)
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(cinema)
		case "/v1/admin/studios":
			if request.Method == http.MethodGet {
				_ = json.NewEncoder(writer).Encode(map[string]any{"studios": state.studios})
				return
			}
			var studio fakeStudio
			_ = json.NewDecoder(request.Body).Decode(&studio)
			studio.ID = "studio-" + string(rune('1'+len(state.studios)))
			state.studios = append(state.studios, studio)
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(studio)
		case "/v1/admin/movies":
			if request.Method == http.MethodGet {
				_ = json.NewEncoder(writer).Encode(map[string]any{"movies": state.movies})
				return
			}
			var movie fakeMovie
			_ = json.NewDecoder(request.Body).Decode(&movie)
			movie.ID = "movie-1"
			state.movies = append(state.movies, movie)
			writer.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(writer).Encode(movie)
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		}
	})
}

// Package seed creates deterministic local cinema catalog data through the protected API.
package seed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultAPIBaseURL = "http://127.0.0.1:18081"
const defaultOMDbBaseURL = "https://www.omdbapi.com/"

// Config contains the local seed command's configuration.
type Config struct {
	APIBaseURL          string
	OMDbBaseURL         string
	OMDbAPIKey          string
	AdminBootstrapToken string
	AdminEmail          string
	AdminPassword       string
	AdminDisplayName    string
}

// ConfigFromEnvironment loads command configuration without logging secrets.
func ConfigFromEnvironment(getenv func(string) string) Config {
	return Config{
		APIBaseURL:          valueOr(getenv("API_BASE_URL"), defaultAPIBaseURL),
		OMDbBaseURL:         valueOr(getenv("OMDB_API_BASE_URL"), defaultOMDbBaseURL),
		OMDbAPIKey:          strings.TrimSpace(getenv("OMDB_API_KEY")),
		AdminBootstrapToken: strings.TrimSpace(getenv("AUTH_ADMIN_BOOTSTRAP_TOKEN")),
		AdminEmail:          valueOr(getenv("SEED_ADMIN_EMAIL"), "admin@cinemas.local"),
		AdminPassword:       valueOr(getenv("SEED_ADMIN_PASSWORD"), "local-admin-password-change-me"),
		AdminDisplayName:    valueOr(getenv("SEED_ADMIN_DISPLAY_NAME"), "Local Administrator"),
	}
}

// Validate rejects missing or malformed seed configuration before any write is attempted.
func (c Config) Validate() error {
	if err := validURL(c.APIBaseURL); err != nil {
		return fmt.Errorf("API_BASE_URL: %w", err)
	}
	if err := validURL(c.OMDbBaseURL); err != nil {
		return fmt.Errorf("OMDB_API_BASE_URL: %w", err)
	}
	if strings.TrimSpace(c.OMDbAPIKey) == "" {
		return fmt.Errorf("OMDB_API_KEY is required")
	}
	if strings.TrimSpace(c.AdminBootstrapToken) == "" {
		return fmt.Errorf("AUTH_ADMIN_BOOTSTRAP_TOKEN is required")
	}
	if strings.TrimSpace(c.AdminEmail) == "" || strings.TrimSpace(c.AdminPassword) == "" || strings.TrimSpace(c.AdminDisplayName) == "" {
		return fmt.Errorf("SEED_ADMIN_EMAIL, SEED_ADMIN_PASSWORD, and SEED_ADMIN_DISPLAY_NAME are required")
	}
	return nil
}

// Cinema is a cinema and the studios to create below it.
type Cinema struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Address string   `json:"address"`
	City    string   `json:"city"`
	Studios []string `json:"-"`
}

// Runner creates catalog fixtures through the API.
type Runner struct {
	client      *http.Client
	cinemas     []Cinema
	movieTitles []string
}

// NewRunner builds a seed runner. The supplied client must have a bounded timeout.
func NewRunner(client *http.Client, cinemas []Cinema, movieTitles []string) *Runner {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &Runner{client: client, cinemas: cinemas, movieTitles: movieTitles}
}

// Run bootstraps or logs into an administrator, then creates missing catalog records.
func (r *Runner) Run(ctx context.Context, config Config) error {
	if err := config.Validate(); err != nil {
		return err
	}
	if err := r.bootstrapAdmin(ctx, config); err != nil {
		return err
	}
	token, err := r.login(ctx, config)
	if err != nil {
		return err
	}
	movies, err := r.prefetchMissingMovies(ctx, config, token)
	if err != nil {
		return err
	}
	if err := r.ensureCinemasAndStudios(ctx, config, token); err != nil {
		return err
	}
	if err := r.createMovies(ctx, config, token, movies); err != nil {
		return err
	}
	return nil
}

func (r *Runner) bootstrapAdmin(ctx context.Context, config Config) error {
	payload := map[string]string{"email": config.AdminEmail, "password": config.AdminPassword, "display_name": config.AdminDisplayName}
	status, err := r.request(ctx, config.APIBaseURL, http.MethodPost, "/v1/auth/bootstrap-admin", "", map[string]string{"X-Admin-Bootstrap-Token": config.AdminBootstrapToken}, payload, nil)
	if status == http.StatusConflict {
		return nil
	}
	if err != nil {
		return fmt.Errorf("bootstrap administrator: %w", err)
	}
	if status != http.StatusCreated {
		return fmt.Errorf("bootstrap administrator: unexpected status %d", status)
	}
	return nil
}

func (r *Runner) login(ctx context.Context, config Config) (string, error) {
	var response struct {
		AccessToken string `json:"access_token"`
		User        struct {
			Role string `json:"role"`
		} `json:"user"`
	}
	_, err := r.request(ctx, config.APIBaseURL, http.MethodPost, "/v1/auth/login", "", nil, map[string]string{"email": config.AdminEmail, "password": config.AdminPassword}, &response)
	if err != nil {
		return "", fmt.Errorf("login administrator: %w", err)
	}
	if response.AccessToken == "" || response.User.Role != "ADMIN" {
		return "", fmt.Errorf("login administrator: configured account is not an administrator")
	}
	return response.AccessToken, nil
}

func (r *Runner) ensureCinemasAndStudios(ctx context.Context, config Config, token string) error {
	var cinemaList struct {
		Cinemas []Cinema `json:"cinemas"`
	}
	if _, err := r.request(ctx, config.APIBaseURL, http.MethodGet, "/v1/admin/cinemas", token, nil, nil, &cinemaList); err != nil {
		return fmt.Errorf("list cinemas: %w", err)
	}
	var studioList struct {
		Studios []studio `json:"studios"`
	}
	if _, err := r.request(ctx, config.APIBaseURL, http.MethodGet, "/v1/admin/studios", token, nil, nil, &studioList); err != nil {
		return fmt.Errorf("list studios: %w", err)
	}
	for _, wanted := range r.cinemas {
		cinema, found := findCinema(cinemaList.Cinemas, wanted.Name)
		if !found {
			var created Cinema
			if _, err := r.request(ctx, config.APIBaseURL, http.MethodPost, "/v1/admin/cinemas", token, nil, wanted, &created); err != nil {
				return fmt.Errorf("create cinema %q: %w", wanted.Name, err)
			}
			cinema = created
			cinemaList.Cinemas = append(cinemaList.Cinemas, created)
		}
		for _, studioName := range wanted.Studios {
			if hasStudio(studioList.Studios, cinema.ID, studioName) {
				continue
			}
			var created studio
			payload := studio{CinemaID: cinema.ID, Name: studioName}
			if _, err := r.request(ctx, config.APIBaseURL, http.MethodPost, "/v1/admin/studios", token, nil, payload, &created); err != nil {
				return fmt.Errorf("create studio %q: %w", studioName, err)
			}
			studioList.Studios = append(studioList.Studios, created)
		}
	}
	return nil
}

func (r *Runner) prefetchMissingMovies(ctx context.Context, config Config, token string) ([]movie, error) {
	var list struct {
		Movies []movie `json:"movies"`
	}
	if _, err := r.request(ctx, config.APIBaseURL, http.MethodGet, "/v1/admin/movies", token, nil, nil, &list); err != nil {
		return nil, fmt.Errorf("list movies: %w", err)
	}
	missing := make([]movie, 0, len(r.movieTitles))
	for _, title := range r.movieTitles {
		if hasMovie(list.Movies, title) {
			continue
		}
		fetched, err := r.fetchOMDbMovie(ctx, config, title)
		if err != nil {
			return nil, fmt.Errorf("load OMDb movie %q: %w", title, err)
		}
		missing = append(missing, fetched)
	}
	return missing, nil
}

func (r *Runner) createMovies(ctx context.Context, config Config, token string, movies []movie) error {
	for _, fetched := range movies {
		var created movie
		if _, err := r.request(ctx, config.APIBaseURL, http.MethodPost, "/v1/admin/movies", token, nil, fetched, &created); err != nil {
			return fmt.Errorf("create movie %q: %w", fetched.Title, err)
		}
	}
	return nil
}

func (r *Runner) fetchOMDbMovie(ctx context.Context, config Config, title string) (movie, error) {
	endpoint, err := url.Parse(config.OMDbBaseURL)
	if err != nil {
		return movie{}, fmt.Errorf("parse OMDb URL: %w", err)
	}
	query := endpoint.Query()
	query.Set("apikey", config.OMDbAPIKey)
	query.Set("t", title)
	query.Set("type", "movie")
	endpoint.RawQuery = query.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return movie{}, fmt.Errorf("create OMDb request: %w", err)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return movie{}, fmt.Errorf("request OMDb: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized {
		return movie{}, fmt.Errorf("OMDb rejected the API key (status 401)")
	}
	if response.StatusCode != http.StatusOK {
		return movie{}, fmt.Errorf("OMDb returned status %d", response.StatusCode)
	}
	var result omdbMovie
	if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
		return movie{}, fmt.Errorf("decode OMDb response: %w", err)
	}
	if result.Response != "True" {
		return movie{}, fmt.Errorf("OMDb did not find movie: %s", result.Error)
	}
	return result.toMovie()
}

func (r *Runner) request(ctx context.Context, baseURL, method, path, token string, headers map[string]string, payload, destination any) (int, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return 0, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(baseURL, "/")+path, body)
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := r.client.Do(request)
	if err != nil {
		return 0, fmt.Errorf("send request: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusBadRequest {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return response.StatusCode, fmt.Errorf("API returned status %d: %s", response.StatusCode, strings.TrimSpace(string(message)))
	}
	if destination != nil && response.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(response.Body).Decode(destination); err != nil {
			return response.StatusCode, fmt.Errorf("decode response: %w", err)
		}
	}
	return response.StatusCode, nil
}

type studio struct {
	ID       string `json:"id"`
	CinemaID string `json:"cinema_id"`
	Name     string `json:"name"`
}

type movie struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	DurationMinutes int    `json:"duration_minutes"`
	Rating          string `json:"rating,omitempty"`
	Synopsis        string `json:"synopsis,omitempty"`
	PosterURL       string `json:"poster_url,omitempty"`
	ReleaseDate     string `json:"release_date,omitempty"`
}

type omdbMovie struct {
	Response string `json:"Response"`
	Error    string `json:"Error"`
	Title    string `json:"Title"`
	Runtime  string `json:"Runtime"`
	Rated    string `json:"Rated"`
	Plot     string `json:"Plot"`
	Poster   string `json:"Poster"`
	Released string `json:"Released"`
}

func (m omdbMovie) toMovie() (movie, error) {
	fields := strings.Fields(m.Runtime)
	if len(fields) == 0 {
		return movie{}, fmt.Errorf("OMDb returned an invalid runtime")
	}
	duration, err := strconv.Atoi(fields[0])
	if err != nil || duration <= 0 {
		return movie{}, fmt.Errorf("OMDb returned an invalid runtime")
	}
	result := movie{Title: strings.TrimSpace(m.Title), DurationMinutes: duration, Rating: optionalOMDbValue(m.Rated), Synopsis: optionalOMDbValue(m.Plot), PosterURL: optionalOMDbValue(m.Poster)}
	if result.Title == "" {
		return movie{}, fmt.Errorf("OMDb returned an empty title")
	}
	if released := optionalOMDbValue(m.Released); released != "" {
		date, err := time.Parse("02 Jan 2006", released)
		if err != nil {
			return movie{}, fmt.Errorf("OMDb returned an invalid release date")
		}
		result.ReleaseDate = date.Format("2006-01-02")
	}
	return result, nil
}

func findCinema(cinemas []Cinema, name string) (Cinema, bool) {
	for _, cinema := range cinemas {
		if sameName(cinema.Name, name) {
			return cinema, true
		}
	}
	return Cinema{}, false
}

func hasStudio(studios []studio, cinemaID, name string) bool {
	for _, studio := range studios {
		if studio.CinemaID == cinemaID && sameName(studio.Name, name) {
			return true
		}
	}
	return false
}

func hasMovie(movies []movie, title string) bool {
	for _, movie := range movies {
		if sameName(movie.Title, title) {
			return true
		}
	}
	return false
}

func sameName(left, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}
func optionalOMDbValue(value string) string {
	if strings.EqualFold(strings.TrimSpace(value), "N/A") {
		return ""
	}
	return strings.TrimSpace(value)
}
func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func validURL(raw string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("must be an absolute URL")
	}
	return nil
}

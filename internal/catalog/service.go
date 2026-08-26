package catalog

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strconv"
	"time"
)

const (
	maxCursorLength = 512
	uuidLength      = 36
)

// Service provides public movie-catalog queries.
type Service struct {
	repository Repository
}

// NewService creates a movie-catalog service backed by the supplied repository.
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// ParsePageSize returns the requested limit or the documented default limit.
func ParsePageSize(value string) (int, error) {
	if value == "" {
		return DefaultPageSize, nil
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit < 1 || limit > maxPageSize {
		return 0, ErrInvalidPageSize
	}
	return limit, nil
}

// ListMovies returns one page of public movie metadata and an opaque next cursor.
func (s *Service) ListMovies(ctx context.Context, input ListInput) (Page, error) {
	if err := ctx.Err(); err != nil {
		return Page{}, err
	}
	if input.Limit < 1 || input.Limit > maxPageSize {
		return Page{}, ErrInvalidPageSize
	}

	cursor, err := decodeCursor(input.Cursor)
	if err != nil {
		return Page{}, err
	}

	movies, err := s.repository.ListMovies(ctx, ListQuery{
		Limit:  input.Limit + 1,
		Cursor: cursor,
	})
	if err != nil {
		return Page{}, err
	}

	page := Page{Movies: movies}
	if len(movies) <= input.Limit {
		return page, nil
	}

	nextCursor, err := encodeCursor(movies[input.Limit-1])
	if err != nil {
		return Page{}, err
	}
	page.Movies = movies[:input.Limit]
	page.NextCursor = nextCursor
	return page, nil
}

type cursorPayload struct {
	CreatedAt string `json:"created_at"`
	ID        string `json:"id"`
}

func encodeCursor(movie Movie) (string, error) {
	payload, err := json.Marshal(cursorPayload{
		CreatedAt: movie.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:        movie.ID,
	})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeCursor(value string) (*Cursor, error) {
	if value == "" {
		return nil, nil
	}
	if len(value) > maxCursorLength {
		return nil, ErrInvalidCursor
	}

	payloadBytes, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, ErrInvalidCursor
	}

	var payload cursorPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil || !isUUID(payload.ID) {
		return nil, ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, payload.CreatedAt)
	if err != nil {
		return nil, ErrInvalidCursor
	}
	return &Cursor{CreatedAt: createdAt, ID: payload.ID}, nil
}

func isUUID(value string) bool {
	if len(value) != uuidLength {
		return false
	}

	for index := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if value[index] != '-' {
				return false
			}
			continue
		}
		if !isHexDigit(value[index]) {
			return false
		}
	}
	return true
}

func isHexDigit(value byte) bool {
	return value >= '0' && value <= '9' || value >= 'a' && value <= 'f' || value >= 'A' && value <= 'F'
}

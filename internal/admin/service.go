package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	uuidByteLength   = 16
	uuidVersionIndex = 6
	uuidVariantIndex = 8
	uuidVersionMask  = 0x0f
	uuidVersion4     = 0x40
	uuidVariantMask  = 0x3f
	uuidVariantRFC   = 0x80
	pricePrecision   = 2
	priceTotalDigits = 12
	currencyCodeSize = 3
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
	cinema := Cinema{
		ID:      id,
		Name:    strings.TrimSpace(input.Name),
		Address: strings.TrimSpace(input.Address),
		City:    strings.TrimSpace(input.City),
	}
	audit := AuditEvent{
		ActorUserID: strings.TrimSpace(input.ActorUserID),
		EntityType:  "CINEMA",
		EntityID:    id,
		Action:      "CREATE",
	}
	return s.repository.CreateCinema(ctx, cinema, audit)
}

// ListCinemas returns all configured cinemas.
func (s *Service) ListCinemas(ctx context.Context) ([]Cinema, error) {
	return s.repository.ListCinemas(ctx)
}

// FindCinema returns one cinema.
func (s *Service) FindCinema(ctx context.Context, id string) (Cinema, error) {
	if strings.TrimSpace(id) == "" {
		return Cinema{}, ErrInvalidCinemaInput
	}
	return s.repository.FindCinema(ctx, strings.TrimSpace(id))
}

// UpdateCinema replaces a cinema and records an audit event.
func (s *Service) UpdateCinema(ctx context.Context, input UpdateCinemaInput) (Cinema, error) {
	actorUserID := strings.TrimSpace(input.ActorUserID)
	id := strings.TrimSpace(input.ID)
	if actorUserID == "" || id == "" || strings.TrimSpace(input.Name) == "" ||
		strings.TrimSpace(input.Address) == "" || strings.TrimSpace(input.City) == "" {
		return Cinema{}, ErrInvalidCinemaInput
	}
	cinema := Cinema{
		ID:      id,
		Name:    strings.TrimSpace(input.Name),
		Address: strings.TrimSpace(input.Address),
		City:    strings.TrimSpace(input.City),
	}
	audit := AuditEvent{
		ActorUserID: actorUserID,
		EntityType:  "CINEMA",
		EntityID:    id,
		Action:      "UPDATE",
	}
	return s.repository.UpdateCinema(ctx, cinema, audit)
}

// DeleteCinema removes a cinema and records an audit event.
func (s *Service) DeleteCinema(ctx context.Context, actorUserID, id string) error {
	actorUserID = strings.TrimSpace(actorUserID)
	id = strings.TrimSpace(id)
	if actorUserID == "" || id == "" {
		return ErrInvalidCinemaInput
	}
	audit := AuditEvent{
		ActorUserID: actorUserID,
		EntityType:  "CINEMA",
		EntityID:    id,
		Action:      "DELETE",
	}
	return s.repository.DeleteCinema(ctx, id, audit)
}

// CreateStudio validates, creates, and audits a studio.
func (s *Service) CreateStudio(ctx context.Context, input CreateStudioInput) (Studio, error) {
	return s.saveStudio(ctx, "", input.ActorUserID, input.CinemaID, input.Name, "CREATE")
}

// ListStudios returns all configured studios.
func (s *Service) ListStudios(ctx context.Context) ([]Studio, error) {
	return s.repository.ListStudios(ctx)
}

// UpdateStudio replaces a studio and records an audit event.
func (s *Service) UpdateStudio(ctx context.Context, input UpdateStudioInput) (Studio, error) {
	return s.saveStudio(ctx, input.ID, input.ActorUserID, input.CinemaID, input.Name, "UPDATE")
}

// DeleteStudio removes a studio and records an audit event.
func (s *Service) DeleteStudio(ctx context.Context, actorID, id string) error {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(id) == "" {
		return ErrInvalidCinemaInput
	}
	return s.repository.DeleteStudio(ctx, id, AuditEvent{ActorUserID: actorID, EntityType: "STUDIO", EntityID: id, Action: "DELETE"}) //nolint:lll // Audit event stays visible at the call site.
}

// CreateSeat validates, creates, and audits a physical seat.
func (s *Service) CreateSeat(ctx context.Context, input CreateSeatInput) (Seat, error) {
	return s.saveSeat(
		ctx,
		"",
		input.ActorUserID,
		input.StudioID,
		input.RowLabel,
		input.SeatNumber,
		input.SeatType,
		"CREATE",
	)
}

// ListSeats returns all configured physical seats.
func (s *Service) ListSeats(ctx context.Context) ([]Seat, error) {
	return s.repository.ListSeats(ctx)
}

// UpdateSeat replaces a physical seat and records an audit event.
func (s *Service) UpdateSeat(ctx context.Context, input UpdateSeatInput) (Seat, error) {
	return s.saveSeat(
		ctx,
		input.ID,
		input.ActorUserID,
		input.StudioID,
		input.RowLabel,
		input.SeatNumber,
		input.SeatType,
		"UPDATE",
	)
}

// DeleteSeat removes a physical seat and records an audit event.
func (s *Service) DeleteSeat(ctx context.Context, actorUserID, id string) error {
	actorUserID = strings.TrimSpace(actorUserID)
	id = strings.TrimSpace(id)
	if actorUserID == "" || id == "" {
		return ErrInvalidSeatInput
	}
	return s.repository.DeleteSeat(ctx, id, AuditEvent{
		ActorUserID: actorUserID,
		EntityType:  "SEAT",
		EntityID:    id,
		Action:      "DELETE",
	})
}

// CreateMovie validates, creates, and audits a movie.
func (s *Service) CreateMovie(ctx context.Context, input CreateMovieInput) (Movie, error) {
	return s.saveMovie(ctx, "", input, "CREATE")
}

// ListMovies returns all configured movies.
func (s *Service) ListMovies(ctx context.Context) ([]Movie, error) {
	return s.repository.ListMovies(ctx)
}

// UpdateMovie replaces a movie and records an audit event.
func (s *Service) UpdateMovie(ctx context.Context, input UpdateMovieInput) (Movie, error) {
	return s.saveMovie(ctx, input.ID, CreateMovieInput{
		ActorUserID:     input.ActorUserID,
		Title:           input.Title,
		DurationMinutes: input.DurationMinutes,
		Rating:          input.Rating,
		Synopsis:        input.Synopsis,
		PosterURL:       input.PosterURL,
		ReleaseDate:     input.ReleaseDate,
	}, "UPDATE")
}

// DeleteMovie removes a movie and records an audit event.
func (s *Service) DeleteMovie(ctx context.Context, actorUserID, id string) error {
	actorUserID = strings.TrimSpace(actorUserID)
	id = strings.TrimSpace(id)
	if actorUserID == "" || id == "" {
		return ErrInvalidMovieInput
	}
	return s.repository.DeleteMovie(ctx, id, AuditEvent{
		ActorUserID: actorUserID,
		EntityType:  "MOVIE",
		EntityID:    id,
		Action:      "DELETE",
	})
}

// CreateShowtime validates, creates, materializes, and audits a showtime.
func (s *Service) CreateShowtime(ctx context.Context, input CreateShowtimeInput) (Showtime, error) {
	return s.saveShowtime(ctx, "", input, "CREATE")
}

// ListShowtimes returns all configured showtimes.
func (s *Service) ListShowtimes(ctx context.Context) ([]Showtime, error) {
	return s.repository.ListShowtimes(ctx)
}

// UpdateShowtime replaces a showtime and its materialized seat snapshot.
func (s *Service) UpdateShowtime(ctx context.Context, input UpdateShowtimeInput) (Showtime, error) {
	return s.saveShowtime(ctx, input.ID, CreateShowtimeInput{
		ActorUserID: input.ActorUserID,
		MovieID:     input.MovieID,
		StudioID:    input.StudioID,
		StartsAt:    input.StartsAt,
		EndsAt:      input.EndsAt,
		BasePrice:   input.BasePrice,
		Currency:    input.Currency,
	}, "UPDATE")
}

// DeleteShowtime removes a showtime and records an audit event.
func (s *Service) DeleteShowtime(ctx context.Context, actorUserID, id string) error {
	actorUserID = strings.TrimSpace(actorUserID)
	id = strings.TrimSpace(id)
	if actorUserID == "" || id == "" {
		return ErrInvalidShowtimeInput
	}
	return s.repository.DeleteShowtime(ctx, id, AuditEvent{
		ActorUserID: actorUserID,
		EntityType:  "SHOWTIME",
		EntityID:    id,
		Action:      "DELETE",
	})
}

func (s *Service) saveStudio(ctx context.Context, id, actorID, cinemaID, name, action string) (Studio, error) {
	if strings.TrimSpace(actorID) == "" || strings.TrimSpace(cinemaID) == "" || strings.TrimSpace(name) == "" {
		return Studio{}, ErrInvalidCinemaInput
	}
	if id == "" {
		var err error
		id, err = s.newID()
		if err != nil {
			return Studio{}, err
		}
	}
	studio := Studio{ID: id, CinemaID: strings.TrimSpace(cinemaID), Name: strings.TrimSpace(name)}
	audit := AuditEvent{ActorUserID: actorID, EntityType: "STUDIO", EntityID: id, Action: action}
	if action == "CREATE" {
		return s.repository.CreateStudio(ctx, studio, audit)
	}
	return s.repository.UpdateStudio(ctx, studio, audit)
}

func (s *Service) saveSeat(
	ctx context.Context,
	id, actorUserID, studioID, rowLabel, seatNumber, seatType, action string,
) (Seat, error) {
	actorUserID = strings.TrimSpace(actorUserID)
	id = strings.TrimSpace(id)
	studioID = strings.TrimSpace(studioID)
	rowLabel = strings.TrimSpace(rowLabel)
	seatNumber = strings.TrimSpace(seatNumber)
	seatType = strings.TrimSpace(seatType)
	if actorUserID == "" || studioID == "" || rowLabel == "" || seatNumber == "" || seatType == "" {
		return Seat{}, ErrInvalidSeatInput
	}
	if id == "" {
		var err error
		id, err = s.newID()
		if err != nil {
			return Seat{}, fmt.Errorf("generate seat id: %w", err)
		}
	}
	seat := Seat{ID: id, StudioID: studioID, RowLabel: rowLabel, SeatNumber: seatNumber, SeatType: seatType}
	audit := AuditEvent{ActorUserID: actorUserID, EntityType: "SEAT", EntityID: id, Action: action}
	if action == "CREATE" {
		return s.repository.CreateSeat(ctx, seat, audit)
	}
	return s.repository.UpdateSeat(ctx, seat, audit)
}

func (s *Service) saveMovie(ctx context.Context, id string, input CreateMovieInput, action string) (Movie, error) {
	actorUserID := strings.TrimSpace(input.ActorUserID)
	id = strings.TrimSpace(id)
	title := strings.TrimSpace(input.Title)
	if actorUserID == "" || title == "" || input.DurationMinutes <= 0 || action == "UPDATE" && id == "" {
		return Movie{}, ErrInvalidMovieInput
	}
	rating := optionalString(input.Rating)
	synopsis := optionalString(input.Synopsis)
	posterURL, err := optionalPosterURL(input.PosterURL)
	if err != nil {
		return Movie{}, ErrInvalidMovieInput
	}
	releaseDate, err := optionalReleaseDate(input.ReleaseDate)
	if err != nil {
		return Movie{}, ErrInvalidMovieInput
	}
	if id == "" {
		id, err = s.newID()
		if err != nil {
			return Movie{}, fmt.Errorf("generate movie id: %w", err)
		}
	}
	movie := Movie{
		ID:              id,
		Title:           title,
		DurationMinutes: input.DurationMinutes,
		Rating:          rating,
		Synopsis:        synopsis,
		PosterURL:       posterURL,
		ReleaseDate:     releaseDate,
	}
	audit := AuditEvent{ActorUserID: actorUserID, EntityType: "MOVIE", EntityID: id, Action: action}
	if action == "CREATE" {
		return s.repository.CreateMovie(ctx, movie, audit)
	}
	return s.repository.UpdateMovie(ctx, movie, audit)
}

func optionalString(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func optionalPosterURL(value *string) (*string, error) {
	posterURL := optionalString(value)
	if posterURL == nil {
		return nil, nil
	}
	parsedURL, err := url.ParseRequestURI(*posterURL)
	if err != nil || !parsedURL.IsAbs() {
		return nil, ErrInvalidMovieInput
	}
	return posterURL, nil
}

func optionalReleaseDate(value *string) (*string, error) {
	releaseDate := optionalString(value)
	if releaseDate == nil {
		return nil, nil
	}
	date, err := time.Parse(time.DateOnly, *releaseDate)
	if err != nil {
		return nil, ErrInvalidMovieInput
	}
	normalized := date.Format(time.DateOnly)
	return &normalized, nil
}

func (s *Service) saveShowtime(
	ctx context.Context,
	id string,
	input CreateShowtimeInput,
	action string,
) (Showtime, error) {
	actorUserID := strings.TrimSpace(input.ActorUserID)
	id = strings.TrimSpace(id)
	movieID := strings.TrimSpace(input.MovieID)
	studioID := strings.TrimSpace(input.StudioID)
	basePrice, err := normalizePrice(input.BasePrice)
	if actorUserID == "" || movieID == "" || studioID == "" || input.StartsAt.IsZero() || input.EndsAt.IsZero() ||
		!input.StartsAt.Before(input.EndsAt) || action == "UPDATE" && id == "" || err != nil {
		return Showtime{}, ErrInvalidShowtimeInput
	}
	currency, ok := normalizeCurrency(input.Currency)
	if !ok {
		return Showtime{}, ErrInvalidShowtimeInput
	}
	if id == "" {
		id, err = s.newID()
		if err != nil {
			return Showtime{}, fmt.Errorf("generate showtime id: %w", err)
		}
	}
	showtime := Showtime{
		ID:        id,
		MovieID:   movieID,
		StudioID:  studioID,
		StartsAt:  input.StartsAt.UTC(),
		EndsAt:    input.EndsAt.UTC(),
		BasePrice: basePrice,
		Currency:  currency,
	}
	audit := AuditEvent{ActorUserID: actorUserID, EntityType: "SHOWTIME", EntityID: id, Action: action}
	if action == "CREATE" {
		return s.repository.CreateShowtime(ctx, showtime, audit)
	}
	return s.repository.UpdateShowtime(ctx, showtime, audit)
}

func normalizePrice(value string) (string, error) {
	value = strings.TrimSpace(value)
	whole, fraction, hasFraction := strings.Cut(value, ".")
	invalidFraction := hasFraction && (fraction == "" || len(fraction) > pricePrecision || !decimalDigits(fraction))
	if whole == "" || !decimalDigits(whole) || invalidFraction {
		return "", ErrInvalidShowtimeInput
	}
	if len(whole)+len(fraction) > priceTotalDigits {
		return "", ErrInvalidShowtimeInput
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	return whole + "." + fraction + strings.Repeat("0", pricePrecision-len(fraction)), nil
}

func decimalDigits(value string) bool {
	for i := range value {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func normalizeCurrency(value string) (string, bool) {
	currency := strings.ToUpper(strings.TrimSpace(value))
	if len(currency) != currencyCodeSize {
		return "", false
	}
	for i := range currency {
		if currency[i] < 'A' || currency[i] > 'Z' {
			return "", false
		}
	}
	return currency, true
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

package httpapi

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	adminservice "github.com/citradigital/cinemas/internal/admin"
	"github.com/citradigital/cinemas/internal/auth"
	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/catalog"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/citradigital/cinemas/internal/scheduling"
	"github.com/citradigital/cinemas/internal/seatinventory"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
)

// Server exposes the cinema HTTP API.
type Server struct {
	e           *echo.Echo
	logger      *slog.Logger
	rateLimiter *clientRateLimiter
}

// NewServer creates an Echo server backed by the booking service.
func NewServer(bookingService *booking.Service) *Server {
	return newServer(bookingService, nil, nil, nil, nil, nil, "")
}

// NewServerWithSeatMap creates an Echo server with booking and seat-map routes.
func NewServerWithSeatMap(
	bookingService *booking.Service,
	seatMapService *seatinventory.Service,
) *Server {
	return newServer(bookingService, seatMapService, nil, nil, nil, nil, "")
}

// NewServerWithMovieCatalog creates an Echo server with public catalog and seat-map routes.
func NewServerWithMovieCatalog(
	bookingService *booking.Service,
	seatMapService *seatinventory.Service,
	movieCatalogService *catalog.Service,
) *Server {
	return newServer(bookingService, seatMapService, movieCatalogService, nil, nil, nil, "")
}

// NewServerWithPublicCatalog creates an Echo server with public catalog and seat-map routes.
func NewServerWithPublicCatalog(
	bookingService *booking.Service,
	seatMapService *seatinventory.Service,
	movieCatalogService *catalog.Service,
	showtimeService *scheduling.Service,
) *Server {
	return newServer(bookingService, seatMapService, movieCatalogService, showtimeService, nil, nil, "")
}

// NewServerWithAllFeatures creates an Echo server with every currently implemented feature.
func NewServerWithAllFeatures(
	bookingService *booking.Service,
	seatMapService *seatinventory.Service,
	movieCatalogService *catalog.Service,
	showtimeService *scheduling.Service,
	paymentService *payments.Service,
	authenticationService *auth.Service,
	adminBootstrapToken string,
) *Server {
	return newServer(
		bookingService,
		seatMapService,
		movieCatalogService,
		showtimeService,
		paymentService,
		authenticationService,
		adminBootstrapToken,
	)
}

// NewServerWithFakePayments creates an Echo server with the local fake payment route.
func NewServerWithFakePayments(bookingService *booking.Service, paymentService *payments.Service) *Server {
	return newServer(bookingService, nil, nil, nil, paymentService, nil, "")
}

// NewServerWithAuth creates an Echo server with customer authentication and protected checkout.
func NewServerWithAuth(
	bookingService *booking.Service,
	authenticationService *auth.Service,
	adminBootstrapToken string,
) *Server {
	return newServer(bookingService, nil, nil, nil, nil, authenticationService, adminBootstrapToken)
}

func newServer(
	bookingService *booking.Service,
	seatMapService *seatinventory.Service,
	movieCatalogService *catalog.Service,
	showtimeService *scheduling.Service,
	paymentService *payments.Service,
	authenticationService *auth.Service,
	adminBootstrapToken string,
) *Server {
	e := echo.New()
	e.HideBanner = true
	e.Use(middleware.RequestID())
	e.Use(middleware.Recover())

	server := &Server{e: e, logger: defaultLogger(), rateLimiter: newClientRateLimiter(time.Now)}
	e.Use(server.requestLogger)
	e.GET("/healthz", server.health)
	if authenticationService == nil {
		e.POST("/v1/orders", server.createOrderHold(bookingService))
	} else {
		e.POST("/v1/auth/register", server.rateLimit(server.register(authenticationService)))
		e.POST("/v1/auth/login", server.rateLimit(server.login(authenticationService)))
		e.POST(
			"/v1/auth/bootstrap-admin",
			server.rateLimit(server.bootstrapAdmin(authenticationService, adminBootstrapToken)),
		)
		e.POST(
			"/v1/orders",
			server.rateLimit(
				server.requireRole(authenticationService, auth.RoleCustomer, server.createOrderHold(bookingService)),
			),
		)
	}
	if paymentService != nil {
		paymentHandler := server.createFakePayment(paymentService)
		if authenticationService != nil {
			paymentHandler = server.requireRole(authenticationService, auth.RoleCustomer, paymentHandler)
		}
		e.POST("/v1/orders/:orderID/payment-intents", server.rateLimit(paymentHandler))
	}
	if seatMapService != nil {
		e.GET("/v1/showtimes/:showtimeID/seats", server.getShowtimeSeats(seatMapService))
	}
	if movieCatalogService != nil {
		e.GET("/v1/movies", server.listMovies(movieCatalogService))
	}
	if showtimeService != nil {
		e.GET("/v1/movies/:movieID/showtimes", server.listMovieShowtimes(showtimeService))
	}
	return server
}

func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.e.ServeHTTP(writer, request)
}

// EnableAdminCinemaRoutes exposes administrator-only cinema management routes.
func (s *Server) EnableAdminCinemaRoutes(authenticationService *auth.Service, service *adminservice.Service) {
	s.e.GET("/v1/admin/cinemas", s.requireRole(authenticationService, auth.RoleAdmin, s.listCinemas(service)))
	s.e.POST("/v1/admin/cinemas", s.requireRole(authenticationService, auth.RoleAdmin, s.createCinema(service)))
	s.e.GET("/v1/admin/cinemas/:cinemaID", s.requireRole(authenticationService, auth.RoleAdmin, s.getCinema(service)))
	s.e.PATCH("/v1/admin/cinemas/:cinemaID", s.requireRole(authenticationService, auth.RoleAdmin, s.updateCinema(service)))
	s.e.DELETE(
		"/v1/admin/cinemas/:cinemaID",
		s.requireRole(authenticationService, auth.RoleAdmin, s.deleteCinema(service)),
	)
	s.e.GET("/v1/admin/studios", s.requireRole(authenticationService, auth.RoleAdmin, s.listStudios(service)))
	s.e.POST("/v1/admin/studios", s.requireRole(authenticationService, auth.RoleAdmin, s.createStudio(service)))
	s.e.PATCH("/v1/admin/studios/:studioID", s.requireRole(authenticationService, auth.RoleAdmin, s.updateStudio(service)))
	//nolint:lll // Route authorization remains explicit.
	s.e.DELETE("/v1/admin/studios/:studioID", s.requireRole(authenticationService, auth.RoleAdmin, s.deleteStudio(service)))
	s.e.GET("/v1/admin/seats", s.requireRole(authenticationService, auth.RoleAdmin, s.listSeats(service)))
	s.e.POST("/v1/admin/seats", s.requireRole(authenticationService, auth.RoleAdmin, s.createSeat(service)))
	s.e.PATCH("/v1/admin/seats/:seatID", s.requireRole(authenticationService, auth.RoleAdmin, s.updateSeat(service)))
	s.e.DELETE("/v1/admin/seats/:seatID", s.requireRole(authenticationService, auth.RoleAdmin, s.deleteSeat(service)))
	s.e.GET("/v1/admin/movies", s.requireRole(authenticationService, auth.RoleAdmin, s.listAdminMovies(service)))
	s.e.POST("/v1/admin/movies", s.requireRole(authenticationService, auth.RoleAdmin, s.createMovie(service)))
	s.e.PATCH("/v1/admin/movies/:movieID", s.requireRole(authenticationService, auth.RoleAdmin, s.updateMovie(service)))
	s.e.DELETE("/v1/admin/movies/:movieID", s.requireRole(authenticationService, auth.RoleAdmin, s.deleteMovie(service)))
	s.e.GET("/v1/admin/showtimes", s.requireRole(authenticationService, auth.RoleAdmin, s.listAdminShowtimes(service)))
	s.e.POST("/v1/admin/showtimes", s.requireRole(authenticationService, auth.RoleAdmin, s.createShowtime(service)))
	s.e.PATCH(
		"/v1/admin/showtimes/:showtimeID",
		s.requireRole(authenticationService, auth.RoleAdmin, s.updateShowtime(service)),
	)
	s.e.DELETE(
		"/v1/admin/showtimes/:showtimeID",
		s.requireRole(authenticationService, auth.RoleAdmin, s.deleteShowtime(service)),
	)
}

type createOrderRequest struct {
	ShowtimeID string   `json:"showtime_id"`
	SeatIDs    []string `json:"seat_ids"`
}

type orderResponse struct {
	ID        string              `json:"id"`
	Status    booking.OrderStatus `json:"status"`
	ExpiresAt string              `json:"expires_at"`
	SeatIDs   []string            `json:"seat_ids"`
}

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

type createCinemaRequest struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
}
type cinemaResponse struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Address string `json:"address"`
	City    string `json:"city"`
}

type cinemaListResponse struct {
	Cinemas []cinemaResponse `json:"cinemas"`
}
type studioRequest struct {
	CinemaID string `json:"cinema_id"`
	Name     string `json:"name"`
}
type studioResponse struct {
	ID       string `json:"id"`
	CinemaID string `json:"cinema_id"`
	Name     string `json:"name"`
}
type studioListResponse struct {
	Studios []studioResponse `json:"studios"`
}
type seatLayoutRequest struct {
	StudioID   string `json:"studio_id"`
	RowLabel   string `json:"row_label"`
	SeatNumber string `json:"seat_number"`
	SeatType   string `json:"seat_type"`
}
type seatLayoutResponse struct {
	ID         string `json:"id"`
	StudioID   string `json:"studio_id"`
	RowLabel   string `json:"row_label"`
	SeatNumber string `json:"seat_number"`
	SeatType   string `json:"seat_type"`
}
type seatLayoutListResponse struct {
	Seats []seatLayoutResponse `json:"seats"`
}
type adminMovieRequest struct {
	Title           string  `json:"title"`
	DurationMinutes int     `json:"duration_minutes"`
	Rating          *string `json:"rating"`
	Synopsis        *string `json:"synopsis"`
	PosterURL       *string `json:"poster_url"`
	ReleaseDate     *string `json:"release_date"`
}
type adminShowtimeRequest struct {
	MovieID   string `json:"movie_id"`
	StudioID  string `json:"studio_id"`
	StartsAt  string `json:"starts_at"`
	EndsAt    string `json:"ends_at"`
	BasePrice string `json:"base_price"`
	Currency  string `json:"currency"`
}
type adminShowtimeResponse struct {
	ID        string `json:"id"`
	MovieID   string `json:"movie_id"`
	StudioID  string `json:"studio_id"`
	StartsAt  string `json:"starts_at"`
	EndsAt    string `json:"ends_at"`
	BasePrice string `json:"base_price"`
	Currency  string `json:"currency"`
}
type adminShowtimeListResponse struct {
	Showtimes []adminShowtimeResponse `json:"showtimes"`
}

type seatMapResponse struct {
	ShowtimeID string         `json:"showtime_id"`
	Seats      []seatResponse `json:"seats"`
}

type seatResponse struct {
	ID          string `json:"id"`
	RowLabel    string `json:"row_label"`
	SeatNumber  string `json:"seat_number"`
	SeatType    string `json:"seat_type"`
	PriceAmount string `json:"price_amount"`
	Currency    string `json:"currency"`
	Status      string `json:"status"`
}

type movieListResponse struct {
	Movies     []movieResponse `json:"movies"`
	NextCursor string          `json:"next_cursor,omitempty"`
}

type movieResponse struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	DurationMinutes int     `json:"duration_minutes"`
	Rating          *string `json:"rating,omitempty"`
	Synopsis        *string `json:"synopsis,omitempty"`
	PosterURL       *string `json:"poster_url,omitempty"`
	ReleaseDate     *string `json:"release_date,omitempty"`
}

type movieShowtimesResponse struct {
	MovieID   string             `json:"movie_id"`
	Date      string             `json:"date"`
	Showtimes []showtimeResponse `json:"showtimes"`
}

type showtimeResponse struct {
	ID         string `json:"id"`
	StudioID   string `json:"studio_id"`
	StudioName string `json:"studio_name"`
	CinemaID   string `json:"cinema_id"`
	CinemaName string `json:"cinema_name"`
	CinemaCity string `json:"cinema_city"`
	StartsAt   string `json:"starts_at"`
	EndsAt     string `json:"ends_at"`
	BasePrice  string `json:"base_price"`
	Currency   string `json:"currency"`
}

type paymentResponse struct {
	Provider  string `json:"provider"`
	Reference string `json:"reference"`
	Status    string `json:"status"`
	Amount    string `json:"amount"`
	Currency  string `json:"currency"`
	PaidAt    string `json:"paid_at"`
}

type authRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type authResponse struct {
	AccessToken string       `json:"access_token"`
	TokenType   string       `json:"token_type"`
	User        userResponse `json:"user"`
}

type userResponse struct {
	ID          string    `json:"id"`
	Email       string    `json:"email"`
	DisplayName string    `json:"display_name"`
	Role        auth.Role `json:"role"`
}

const uuidLength = 36

func (s *Server) health(c echo.Context) error {
	return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) createOrderHold(service *booking.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		idempotencyKey := c.Request().Header.Get("Idempotency-Key")
		if idempotencyKey == "" {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "Idempotency-Key is required")
		}

		var request createOrderRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}

		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}

		order, err := service.CreateHold(c.Request().Context(), booking.CreateHoldInput{
			UserID:         identity.UserID,
			ShowtimeID:     request.ShowtimeID,
			SeatIDs:        request.SeatIDs,
			IdempotencyKey: idempotencyKey,
		})
		if err != nil {
			return writeBookingError(c, err)
		}

		seatIDs := make([]string, len(order.Items))
		for i, item := range order.Items {
			seatIDs[i] = item.SeatID
		}
		return c.JSON(http.StatusCreated, orderResponse{
			ID:        order.ID,
			Status:    order.Status,
			ExpiresAt: order.ExpiresAt.Format("2006-01-02T15:04:05Z07:00"),
			SeatIDs:   seatIDs,
		})
	}
}

func (s *Server) createCinema(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var request createCinemaRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		cinema, err := service.CreateCinema(c.Request().Context(), adminservice.CreateCinemaInput{
			ActorUserID: identity.UserID,
			Name:        request.Name,
			Address:     request.Address,
			City:        request.City,
		})
		if err != nil {
			return writeCinemaError(c, err, "create")
		}
		return c.JSON(http.StatusCreated, toCinemaResponse(cinema))
	}
}

func (s *Server) listCinemas(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		cinemas, err := service.ListCinemas(c.Request().Context())
		if err != nil {
			return writeCinemaError(c, err, "list")
		}

		response := make([]cinemaResponse, len(cinemas))
		for i, cinema := range cinemas {
			response[i] = toCinemaResponse(cinema)
		}
		return c.JSON(http.StatusOK, cinemaListResponse{Cinemas: response})
	}
}

func (s *Server) getCinema(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		cinemaID, err := cinemaIDParam(c)
		if err != nil {
			return err
		}
		cinema, err := service.FindCinema(c.Request().Context(), cinemaID)
		if err != nil {
			return writeCinemaError(c, err, "load")
		}
		return c.JSON(http.StatusOK, toCinemaResponse(cinema))
	}
}

func (s *Server) updateCinema(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		cinemaID, err := cinemaIDParam(c)
		if err != nil {
			return err
		}
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var request createCinemaRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		cinema, err := service.UpdateCinema(c.Request().Context(), adminservice.UpdateCinemaInput{
			ActorUserID: identity.UserID,
			ID:          cinemaID,
			Name:        request.Name,
			Address:     request.Address,
			City:        request.City,
		})
		if err != nil {
			return writeCinemaError(c, err, "update")
		}
		return c.JSON(http.StatusOK, toCinemaResponse(cinema))
	}
}

func (s *Server) deleteCinema(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		cinemaID, err := cinemaIDParam(c)
		if err != nil {
			return err
		}
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		if err := service.DeleteCinema(c.Request().Context(), identity.UserID, cinemaID); err != nil {
			return writeCinemaError(c, err, "delete")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func cinemaIDParam(c echo.Context) (string, error) {
	cinemaID := c.Param("cinemaID")
	if !isUUID(cinemaID) {
		return "", writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "cinema ID must be a UUID")
	}
	return cinemaID, nil
}

func toCinemaResponse(cinema adminservice.Cinema) cinemaResponse {
	return cinemaResponse{
		ID:      cinema.ID,
		Name:    cinema.Name,
		Address: cinema.Address,
		City:    cinema.City,
	}
}

func writeCinemaError(c echo.Context, err error, operation string) error {
	switch {
	case errors.Is(err, adminservice.ErrInvalidCinemaInput):
		return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "name, address, and city are required")
	case errors.Is(err, adminservice.ErrCinemaNotFound):
		return writeError(c, http.StatusNotFound, "CINEMA_NOT_FOUND", "cinema was not found")
	default:
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to "+operation+" cinema")
	}
}

func (s *Server) createStudio(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var req studioRequest
		if err := c.Bind(&req); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		studio, err := service.CreateStudio(c.Request().Context(), adminservice.CreateStudioInput{ActorUserID: identity.UserID, CinemaID: req.CinemaID, Name: req.Name}) //nolint:lll // Transport fields are mapped together.
		if err != nil {
			return writeStudioError(c, err)
		}
		return c.JSON(http.StatusCreated, toStudioResponse(studio))
	}
}
func (s *Server) listStudios(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		studios, err := service.ListStudios(c.Request().Context())
		if err != nil {
			return writeStudioError(c, err)
		}
		result := make([]studioResponse, len(studios))
		for i, studio := range studios {
			result[i] = toStudioResponse(studio)
		}
		return c.JSON(http.StatusOK, studioListResponse{Studios: result})
	}
}
func (s *Server) updateStudio(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		id := c.Param("studioID")
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var req studioRequest
		if err := c.Bind(&req); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		studio, err := service.UpdateStudio(c.Request().Context(), adminservice.UpdateStudioInput{ActorUserID: identity.UserID, ID: id, CinemaID: req.CinemaID, Name: req.Name}) //nolint:lll // Transport fields are mapped together.
		if err != nil {
			return writeStudioError(c, err)
		}
		return c.JSON(http.StatusOK, toStudioResponse(studio))
	}
}
func (s *Server) deleteStudio(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		if err := service.DeleteStudio(c.Request().Context(), identity.UserID, c.Param("studioID")); err != nil {
			return writeStudioError(c, err)
		}
		return c.NoContent(http.StatusNoContent)
	}
}
func toStudioResponse(s adminservice.Studio) studioResponse {
	return studioResponse{ID: s.ID, CinemaID: s.CinemaID, Name: s.Name}
}
func writeStudioError(c echo.Context, err error) error {
	if errors.Is(err, adminservice.ErrStudioNotFound) {
		return writeError(c, http.StatusNotFound, "STUDIO_NOT_FOUND", "studio was not found")
	}
	if errors.Is(err, adminservice.ErrCinemaNotFound) {
		return writeError(c, http.StatusNotFound, "CINEMA_NOT_FOUND", "cinema was not found")
	}
	if errors.Is(err, adminservice.ErrInvalidCinemaInput) {
		return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "cinema ID and name are required")
	}
	return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to manage studio")
}

func (s *Server) createSeat(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var request seatLayoutRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		if !isUUID(request.StudioID) {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "studio ID must be a UUID")
		}
		seat, err := service.CreateSeat(c.Request().Context(), adminservice.CreateSeatInput{
			ActorUserID: identity.UserID,
			StudioID:    request.StudioID,
			RowLabel:    request.RowLabel,
			SeatNumber:  request.SeatNumber,
			SeatType:    request.SeatType,
		})
		if err != nil {
			return writeSeatError(c, err, "create")
		}
		return c.JSON(http.StatusCreated, toSeatLayoutResponse(seat))
	}
}

func (s *Server) listSeats(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		seats, err := service.ListSeats(c.Request().Context())
		if err != nil {
			return writeSeatError(c, err, "list")
		}
		response := make([]seatLayoutResponse, len(seats))
		for i, seat := range seats {
			response[i] = toSeatLayoutResponse(seat)
		}
		return c.JSON(http.StatusOK, seatLayoutListResponse{Seats: response})
	}
}

func (s *Server) updateSeat(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		seatID, err := seatIDParam(c)
		if err != nil {
			return err
		}
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var request seatLayoutRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		if !isUUID(request.StudioID) {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "studio ID must be a UUID")
		}
		seat, err := service.UpdateSeat(c.Request().Context(), adminservice.UpdateSeatInput{
			ActorUserID: identity.UserID,
			ID:          seatID,
			StudioID:    request.StudioID,
			RowLabel:    request.RowLabel,
			SeatNumber:  request.SeatNumber,
			SeatType:    request.SeatType,
		})
		if err != nil {
			return writeSeatError(c, err, "update")
		}
		return c.JSON(http.StatusOK, toSeatLayoutResponse(seat))
	}
}

func (s *Server) deleteSeat(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		seatID, err := seatIDParam(c)
		if err != nil {
			return err
		}
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		if err := service.DeleteSeat(c.Request().Context(), identity.UserID, seatID); err != nil {
			return writeSeatError(c, err, "delete")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func seatIDParam(c echo.Context) (string, error) {
	seatID := c.Param("seatID")
	if !isUUID(seatID) {
		return "", writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "seat ID must be a UUID")
	}
	return seatID, nil
}

func toSeatLayoutResponse(seat adminservice.Seat) seatLayoutResponse {
	return seatLayoutResponse{
		ID:         seat.ID,
		StudioID:   seat.StudioID,
		RowLabel:   seat.RowLabel,
		SeatNumber: seat.SeatNumber,
		SeatType:   seat.SeatType,
	}
}

func writeSeatError(c echo.Context, err error, operation string) error {
	switch {
	case errors.Is(err, adminservice.ErrInvalidSeatInput):
		return writeError(
			c,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"studio ID, row label, seat number, and seat type are required",
		)
	case errors.Is(err, adminservice.ErrStudioNotFound):
		return writeError(c, http.StatusNotFound, "STUDIO_NOT_FOUND", "studio was not found")
	case errors.Is(err, adminservice.ErrSeatNotFound):
		return writeError(c, http.StatusNotFound, "SEAT_NOT_FOUND", "seat was not found")
	case errors.Is(err, adminservice.ErrSeatAlreadyExists):
		return writeError(c, http.StatusConflict, "SEAT_ALREADY_EXISTS", "seat position already exists in this studio")
	case errors.Is(err, adminservice.ErrSeatLayoutInUse):
		return writeError(c, http.StatusConflict, "SEAT_LAYOUT_IN_USE", "seat layout cannot change after a showtime exists")
	default:
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to "+operation+" seat")
	}
}

func (s *Server) createMovie(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var request adminMovieRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		movie, err := service.CreateMovie(c.Request().Context(), toCreateMovieInput(identity.UserID, request))
		if err != nil {
			return writeMovieError(c, err, "create")
		}
		return c.JSON(http.StatusCreated, toAdminMovieResponse(movie))
	}
}

func (s *Server) listAdminMovies(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		movies, err := service.ListMovies(c.Request().Context())
		if err != nil {
			return writeMovieError(c, err, "list")
		}
		response := make([]movieResponse, len(movies))
		for i, movie := range movies {
			response[i] = toAdminMovieResponse(movie)
		}
		return c.JSON(http.StatusOK, movieListResponse{Movies: response})
	}
}

func (s *Server) updateMovie(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		movieID, err := movieIDParam(c)
		if err != nil {
			return err
		}
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var request adminMovieRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		input := toCreateMovieInput(identity.UserID, request)
		movie, err := service.UpdateMovie(c.Request().Context(), adminservice.UpdateMovieInput{
			ActorUserID:     input.ActorUserID,
			ID:              movieID,
			Title:           input.Title,
			DurationMinutes: input.DurationMinutes,
			Rating:          input.Rating,
			Synopsis:        input.Synopsis,
			PosterURL:       input.PosterURL,
			ReleaseDate:     input.ReleaseDate,
		})
		if err != nil {
			return writeMovieError(c, err, "update")
		}
		return c.JSON(http.StatusOK, toAdminMovieResponse(movie))
	}
}

func (s *Server) deleteMovie(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		movieID, err := movieIDParam(c)
		if err != nil {
			return err
		}
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		if err := service.DeleteMovie(c.Request().Context(), identity.UserID, movieID); err != nil {
			return writeMovieError(c, err, "delete")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func movieIDParam(c echo.Context) (string, error) {
	movieID := c.Param("movieID")
	if !isUUID(movieID) {
		return "", writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "movie ID must be a UUID")
	}
	return movieID, nil
}

func toCreateMovieInput(actorUserID string, request adminMovieRequest) adminservice.CreateMovieInput {
	return adminservice.CreateMovieInput{
		ActorUserID:     actorUserID,
		Title:           request.Title,
		DurationMinutes: request.DurationMinutes,
		Rating:          request.Rating,
		Synopsis:        request.Synopsis,
		PosterURL:       request.PosterURL,
		ReleaseDate:     request.ReleaseDate,
	}
}

func toAdminMovieResponse(movie adminservice.Movie) movieResponse {
	return movieResponse{
		ID:              movie.ID,
		Title:           movie.Title,
		DurationMinutes: movie.DurationMinutes,
		Rating:          movie.Rating,
		Synopsis:        movie.Synopsis,
		PosterURL:       movie.PosterURL,
		ReleaseDate:     movie.ReleaseDate,
	}
}

func writeMovieError(c echo.Context, err error, operation string) error {
	switch {
	case errors.Is(err, adminservice.ErrInvalidMovieInput):
		return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "movie metadata is invalid")
	case errors.Is(err, adminservice.ErrMovieNotFound):
		return writeError(c, http.StatusNotFound, "MOVIE_NOT_FOUND", "movie was not found")
	default:
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to "+operation+" movie")
	}
}

func (s *Server) createShowtime(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var request adminShowtimeRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		input, err := toCreateShowtimeInput(identity.UserID, request)
		if err != nil {
			return writeShowtimeError(c, err, "create")
		}
		showtime, err := service.CreateShowtime(c.Request().Context(), input)
		if err != nil {
			return writeShowtimeError(c, err, "create")
		}
		return c.JSON(http.StatusCreated, toAdminShowtimeResponse(showtime))
	}
}

func (s *Server) listAdminShowtimes(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		showtimes, err := service.ListShowtimes(c.Request().Context())
		if err != nil {
			return writeShowtimeError(c, err, "list")
		}
		response := make([]adminShowtimeResponse, len(showtimes))
		for i, showtime := range showtimes {
			response[i] = toAdminShowtimeResponse(showtime)
		}
		return c.JSON(http.StatusOK, adminShowtimeListResponse{Showtimes: response})
	}
}

func (s *Server) updateShowtime(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		showtimeID, err := showtimeIDParam(c)
		if err != nil {
			return err
		}
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		var request adminShowtimeRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		input, err := toCreateShowtimeInput(identity.UserID, request)
		if err != nil {
			return writeShowtimeError(c, err, "update")
		}
		showtime, err := service.UpdateShowtime(c.Request().Context(), adminservice.UpdateShowtimeInput{
			ActorUserID: input.ActorUserID,
			ID:          showtimeID,
			MovieID:     input.MovieID,
			StudioID:    input.StudioID,
			StartsAt:    input.StartsAt,
			EndsAt:      input.EndsAt,
			BasePrice:   input.BasePrice,
			Currency:    input.Currency,
		})
		if err != nil {
			return writeShowtimeError(c, err, "update")
		}
		return c.JSON(http.StatusOK, toAdminShowtimeResponse(showtime))
	}
}

func (s *Server) deleteShowtime(service *adminservice.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		showtimeID, err := showtimeIDParam(c)
		if err != nil {
			return err
		}
		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		if err := service.DeleteShowtime(c.Request().Context(), identity.UserID, showtimeID); err != nil {
			return writeShowtimeError(c, err, "delete")
		}
		return c.NoContent(http.StatusNoContent)
	}
}

func toCreateShowtimeInput(
	actorUserID string,
	request adminShowtimeRequest,
) (adminservice.CreateShowtimeInput, error) {
	if !isUUID(request.MovieID) || !isUUID(request.StudioID) {
		return adminservice.CreateShowtimeInput{}, adminservice.ErrInvalidShowtimeInput
	}
	startsAt, err := time.Parse(time.RFC3339, request.StartsAt)
	if err != nil {
		return adminservice.CreateShowtimeInput{}, adminservice.ErrInvalidShowtimeInput
	}
	endsAt, err := time.Parse(time.RFC3339, request.EndsAt)
	if err != nil {
		return adminservice.CreateShowtimeInput{}, adminservice.ErrInvalidShowtimeInput
	}
	return adminservice.CreateShowtimeInput{
		ActorUserID: actorUserID,
		MovieID:     request.MovieID,
		StudioID:    request.StudioID,
		StartsAt:    startsAt,
		EndsAt:      endsAt,
		BasePrice:   request.BasePrice,
		Currency:    request.Currency,
	}, nil
}

func showtimeIDParam(c echo.Context) (string, error) {
	showtimeID := c.Param("showtimeID")
	if !isUUID(showtimeID) {
		return "", writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "showtime ID must be a UUID")
	}
	return showtimeID, nil
}

func toAdminShowtimeResponse(showtime adminservice.Showtime) adminShowtimeResponse {
	return adminShowtimeResponse{
		ID:        showtime.ID,
		MovieID:   showtime.MovieID,
		StudioID:  showtime.StudioID,
		StartsAt:  showtime.StartsAt.Format(time.RFC3339),
		EndsAt:    showtime.EndsAt.Format(time.RFC3339),
		BasePrice: showtime.BasePrice,
		Currency:  showtime.Currency,
	}
}

func writeShowtimeError(c echo.Context, err error, operation string) error {
	switch {
	case errors.Is(err, adminservice.ErrInvalidShowtimeInput):
		return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "showtime metadata is invalid")
	case errors.Is(err, adminservice.ErrMovieNotFound):
		return writeError(c, http.StatusNotFound, "MOVIE_NOT_FOUND", "movie was not found")
	case errors.Is(err, adminservice.ErrStudioNotFound):
		return writeError(c, http.StatusNotFound, "STUDIO_NOT_FOUND", "studio was not found")
	case errors.Is(err, adminservice.ErrShowtimeNotFound):
		return writeError(c, http.StatusNotFound, "SHOWTIME_NOT_FOUND", "showtime was not found")
	case errors.Is(err, adminservice.ErrShowtimeInUse):
		return writeError(c, http.StatusConflict, "SHOWTIME_IN_USE", "showtime inventory has already been used")
	case errors.Is(err, adminservice.ErrShowtimeOverlap):
		return writeError(c, http.StatusConflict, "SHOWTIME_OVERLAP", "showtime overlaps another screening in this studio")
	default:
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to "+operation+" showtime")
	}
}

func (s *Server) createFakePayment(service *payments.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		orderID := c.Param("orderID")
		if !isUUID(orderID) {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "order ID must be a UUID")
		}

		identity, ok := authenticatedIdentity(c)
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}

		payment, err := service.CreateFakePayment(c.Request().Context(), orderID, identity.UserID)
		if err != nil {
			switch {
			case errors.Is(err, payments.ErrOrderNotFound):
				return writeError(c, http.StatusNotFound, "ORDER_NOT_FOUND", "order was not found")
			case errors.Is(err, payments.ErrOrderNotPayable), errors.Is(err, payments.ErrOrderExpired):
				return writeError(c, http.StatusConflict, "ORDER_NOT_PAYABLE", "order can no longer be paid")
			default:
				return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to complete payment")
			}
		}

		return c.JSON(http.StatusCreated, paymentResponse{
			Provider:  payment.Provider,
			Reference: payment.Reference,
			Status:    string(payment.Status),
			Amount:    payment.Amount,
			Currency:  payment.Currency,
			PaidAt:    payment.PaidAt.Format(time.RFC3339),
		})
	}
}

func (s *Server) register(service *auth.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		var request authRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		session, err := service.Register(c.Request().Context(), toRegisterInput(request))
		if err != nil {
			return writeAuthError(c, err)
		}
		return c.JSON(http.StatusCreated, toAuthResponse(session))
	}
}

func (s *Server) login(service *auth.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		var request authRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		session, err := service.Login(c.Request().Context(), auth.LoginInput{
			Email:    request.Email,
			Password: request.Password,
		})
		if err != nil {
			return writeAuthError(c, err)
		}
		return c.JSON(http.StatusOK, toAuthResponse(session))
	}
}

func (s *Server) bootstrapAdmin(service *auth.Service, bootstrapToken string) echo.HandlerFunc {
	return func(c echo.Context) error {
		providedToken := c.Request().Header.Get("X-Admin-Bootstrap-Token")
		if bootstrapToken == "" || subtle.ConstantTimeCompare([]byte(providedToken), []byte(bootstrapToken)) != 1 {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "bootstrap token is invalid")
		}
		var request authRequest
		if err := c.Bind(&request); err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		}
		session, err := service.RegisterAdmin(c.Request().Context(), toRegisterInput(request))
		if err != nil {
			return writeAuthError(c, err)
		}
		return c.JSON(http.StatusCreated, toAuthResponse(session))
	}
}

func (s *Server) requireRole(service *auth.Service, role auth.Role, next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) error {
		accessToken, ok := bearerToken(c.Request().Header.Get(echo.HeaderAuthorization))
		if !ok {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is required")
		}
		identity, err := service.Authenticate(c.Request().Context(), accessToken)
		if err != nil {
			return writeError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "access token is invalid or expired")
		}
		if identity.Role != role {
			return writeError(c, http.StatusForbidden, "FORBIDDEN", "insufficient permissions")
		}
		c.Set("authenticated_identity", identity)
		return next(c)
	}
}

func bearerToken(value string) (string, bool) {
	parts := strings.Fields(value)
	returnToken := ""
	if len(parts) == 2 && parts[0] == "Bearer" && parts[1] != "" {
		returnToken = parts[1]
	}
	return returnToken, returnToken != ""
}

func authenticatedIdentity(c echo.Context) (auth.Identity, bool) {
	identity, ok := c.Get("authenticated_identity").(auth.Identity)
	return identity, ok
}

func toRegisterInput(request authRequest) auth.RegisterInput {
	return auth.RegisterInput{
		Email:       request.Email,
		Password:    request.Password,
		DisplayName: request.DisplayName,
	}
}

func toAuthResponse(session auth.Session) authResponse {
	return authResponse{
		AccessToken: session.AccessToken,
		TokenType:   "Bearer",
		User: userResponse{
			ID:          session.User.ID,
			Email:       session.User.Email,
			DisplayName: session.User.DisplayName,
			Role:        session.User.Role,
		},
	}
}

func (s *Server) getShowtimeSeats(service *seatinventory.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		showtimeID := c.Param("showtimeID")
		if !isUUID(showtimeID) {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "showtime ID must be a UUID")
		}

		seats, err := service.ListSeatMap(c.Request().Context(), showtimeID)
		if err != nil {
			if errors.Is(err, seatinventory.ErrShowtimeNotFound) {
				return writeError(c, http.StatusNotFound, "SHOWTIME_NOT_FOUND", "showtime was not found")
			}
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to load seat map")
		}

		responseSeats := make([]seatResponse, len(seats))
		for i, seat := range seats {
			responseSeats[i] = seatResponse{
				ID:          seat.ID,
				RowLabel:    seat.RowLabel,
				SeatNumber:  seat.SeatNumber,
				SeatType:    seat.SeatType,
				PriceAmount: seat.PriceAmount,
				Currency:    seat.Currency,
				Status:      seat.Status,
			}
		}
		return c.JSON(http.StatusOK, seatMapResponse{
			ShowtimeID: showtimeID,
			Seats:      responseSeats,
		})
	}
}

func (s *Server) listMovies(service *catalog.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		limit, err := catalog.ParsePageSize(c.QueryParam("limit"))
		if err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "limit must be between 1 and 100")
		}

		page, err := service.ListMovies(c.Request().Context(), catalog.ListInput{
			Limit:  limit,
			Cursor: c.QueryParam("cursor"),
		})
		if err != nil {
			if errors.Is(err, catalog.ErrInvalidCursor) || errors.Is(err, catalog.ErrInvalidPageSize) {
				return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "cursor or limit is invalid")
			}
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to load movies")
		}

		movies := make([]movieResponse, len(page.Movies))
		for i, movie := range page.Movies {
			movies[i] = movieResponse{
				ID:              movie.ID,
				Title:           movie.Title,
				DurationMinutes: movie.DurationMinutes,
				Rating:          movie.Rating,
				Synopsis:        movie.Synopsis,
				PosterURL:       movie.PosterURL,
				ReleaseDate:     movie.ReleaseDate,
			}
		}

		return c.JSON(http.StatusOK, movieListResponse{
			Movies:     movies,
			NextCursor: page.NextCursor,
		})
	}
}

func (s *Server) listMovieShowtimes(service *scheduling.Service) echo.HandlerFunc {
	return func(c echo.Context) error {
		movieID := c.Param("movieID")
		if !isUUID(movieID) {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "movie ID must be a UUID")
		}

		date, err := scheduling.ParseDate(c.QueryParam("date"))
		if err != nil {
			return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "date must use YYYY-MM-DD")
		}

		showtimes, err := service.ListMovieShowtimes(c.Request().Context(), scheduling.ListInput{
			MovieID: movieID,
			Date:    date,
		})
		if err != nil {
			if errors.Is(err, scheduling.ErrMovieNotFound) {
				return writeError(c, http.StatusNotFound, "MOVIE_NOT_FOUND", "movie was not found")
			}
			if errors.Is(err, scheduling.ErrInvalidDate) {
				return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "date must use YYYY-MM-DD")
			}
			return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to load movie showtimes")
		}

		responseShowtimes := make([]showtimeResponse, len(showtimes))
		for i, showtime := range showtimes {
			responseShowtimes[i] = showtimeResponse{
				ID:         showtime.ID,
				StudioID:   showtime.StudioID,
				StudioName: showtime.StudioName,
				CinemaID:   showtime.CinemaID,
				CinemaName: showtime.CinemaName,
				CinemaCity: showtime.CinemaCity,
				StartsAt:   showtime.StartsAt.Format(time.RFC3339),
				EndsAt:     showtime.EndsAt.Format(time.RFC3339),
				BasePrice:  showtime.BasePrice,
				Currency:   showtime.Currency,
			}
		}

		return c.JSON(http.StatusOK, movieShowtimesResponse{
			MovieID:   movieID,
			Date:      date.Format(time.DateOnly),
			Showtimes: responseShowtimes,
		})
	}
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

func writeBookingError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, booking.ErrInvalidHoldInput):
		return writeError(
			c,
			http.StatusBadRequest,
			"INVALID_REQUEST",
			"user, showtime, and at least one unique seat are required",
		)
	case errors.Is(err, booking.ErrSeatUnavailable):
		return writeError(c, http.StatusConflict, "SEAT_UNAVAILABLE", "one or more selected seats are no longer available")
	case errors.Is(err, booking.ErrIdempotencyKeyReused):
		return writeError(
			c,
			http.StatusConflict,
			"IDEMPOTENCY_KEY_REUSED",
			"Idempotency-Key was used with a different request",
		)
	default:
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to create order")
	}
}

func writeAuthError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidInput):
		return writeError(c, http.StatusBadRequest, "INVALID_REQUEST", "email, password, and display name are invalid")
	case errors.Is(err, auth.ErrEmailAlreadyRegistered):
		return writeError(c, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "email is already registered")
	case errors.Is(err, auth.ErrInvalidCredentials):
		return writeError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "email or password is incorrect")
	case errors.Is(err, auth.ErrAdminAlreadyBootstrapped):
		return writeError(c, http.StatusConflict, "ADMIN_ALREADY_BOOTSTRAPPED", "an initial admin already exists")
	default:
		return writeError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "unable to complete authentication")
	}
}

func writeError(c echo.Context, status int, code, message string) error {
	return c.JSON(status, errorResponse{
		Code:      code,
		Message:   message,
		RequestID: c.Response().Header().Get(echo.HeaderXRequestID),
	})
}

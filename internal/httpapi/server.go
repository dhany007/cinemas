package httpapi

import (
	"crypto/subtle"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

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

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/citradigital/cinemas/internal/auth"
	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/catalog"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/citradigital/cinemas/internal/scheduling"
	"github.com/citradigital/cinemas/internal/seatinventory"
)

func TestServerLogsRequestMetadataWithoutRequestBody(t *testing.T) {
	service := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	server := NewServer(service)
	var logs bytes.Buffer
	server.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/healthz?token=not-logged", nil)

	server.ServeHTTP(recorder, request)

	if !strings.Contains(logs.String(), `"route":"/healthz"`) {
		t.Fatalf("logs = %s, want health route", logs.String())
	}
	if strings.Contains(logs.String(), "not-logged") {
		t.Fatalf("logs contain request query: %s", logs.String())
	}
}

func TestServerRegistersCustomerAndUsesTokenIdentityForCheckout(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	bookingRepository := booking.NewMemoryRepository([]booking.Seat{{
		ID: "seat-a", ShowtimeID: "showtime-1", Status: booking.SeatAvailable,
	}})
	bookingService := booking.NewService(bookingRepository, 10*time.Minute, func() time.Time { return now })
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		func() time.Time { return now },
	)
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")

	registerRecorder := httptest.NewRecorder()
	registerRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/register",
		bytes.NewBufferString(
			`{"email":"customer@example.com",`+
				`"password":"correct horse battery staple","display_name":"Customer"}`,
		),
	)
	registerRequest.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(registerRecorder, registerRequest)
	if registerRecorder.Code != http.StatusCreated {
		t.Fatalf(
			"register status = %d, want %d; body = %s",
			registerRecorder.Code,
			http.StatusCreated,
			registerRecorder.Body.String(),
		)
	}

	var registered struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(registerRecorder.Body.Bytes(), &registered); err != nil {
		t.Fatalf("unmarshal register response: %v", err)
	}

	orderRecorder := httptest.NewRecorder()
	orderRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/orders",
		bytes.NewBufferString(`{"user_id":"attacker-id","showtime_id":"showtime-1","seat_ids":["seat-a"]}`),
	)
	orderRequest.Header.Set("Content-Type", "application/json")
	orderRequest.Header.Set("Idempotency-Key", "checkout-1")
	orderRequest.Header.Set("Authorization", "Bearer "+registered.AccessToken)
	server.ServeHTTP(orderRecorder, orderRequest)
	if orderRecorder.Code != http.StatusCreated {
		t.Fatalf("order status = %d, want %d; body = %s", orderRecorder.Code, http.StatusCreated, orderRecorder.Body.String())
	}

	order, found, err := bookingRepository.FindOrderByIdempotency(context.Background(), registered.User.ID, "checkout-1")
	if err != nil || !found {
		t.Fatalf("FindOrderByIdempotency() found = %t, err = %v", found, err)
	}
	if order.UserID != registered.User.ID {
		t.Fatalf("order user ID = %q, want authenticated user %q", order.UserID, registered.User.ID)
	}
}

func TestServerRejectsCheckoutWithoutAccessToken(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/orders", bytes.NewBufferString(`{}`))

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if want := `"code":"UNAUTHENTICATED"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerRejectsAdminBootstrapWithoutConfiguredToken(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/auth/bootstrap-admin",
		bytes.NewBufferString(
			`{"email":"admin@example.com",`+
				`"password":"correct horse battery staple","display_name":"Admin"}`,
		),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Admin-Bootstrap-Token", "wrong-token")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if want := `"code":"UNAUTHENTICATED"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerRateLimitsLogin(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	for requestCount := 0; requestCount < rateLimitMaxRequests; requestCount++ {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewBufferString(`{}`))
		request.RemoteAddr = "192.0.2.1:1234"
		server.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("request %d status = %d, want %d", requestCount, recorder.Code, http.StatusBadRequest)
		}
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/auth/login", bytes.NewBufferString(`{}`))
	request.RemoteAddr = "192.0.2.1:1234"
	server.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusTooManyRequests)
	}
}

func TestServerCreateOrderHold(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	service := booking.NewService(
		booking.NewMemoryRepository([]booking.Seat{{ID: "seat-a", ShowtimeID: "showtime-1", Status: booking.SeatAvailable}}),
		10*time.Minute,
		func() time.Time { return now },
	)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		func() time.Time { return now },
	)
	session, err := authenticationService.Register(context.Background(), auth.RegisterInput{
		Email:       "customer@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Customer",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	server := NewServerWithAuth(service, authenticationService, "bootstrap-token")
	recorder := httptest.NewRecorder()
	requestBody := `{"showtime_id":"showtime-1","seat_ids":["seat-a"]}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/orders",
		bytes.NewBufferString(requestBody),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "checkout-1")
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
	if want := `"status":"PENDING_PAYMENT"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerCreateOrderHoldReturnsStableValidationError(t *testing.T) {
	service := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	server := NewServer(service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/v1/orders", bytes.NewBufferString(`{}`))
	request.Header.Set("Content-Type", "application/json")

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if want := `"code":"INVALID_REQUEST"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerGetShowtimeSeats(t *testing.T) {
	showtimeID := "10000000-0000-4000-8000-000000000001"
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(map[string][]seatinventory.Seat{
		showtimeID: {
			{
				ID:          "seat-a",
				RowLabel:    "A",
				SeatNumber:  "1",
				SeatType:    "STANDARD",
				PriceAmount: "50000.00",
				Currency:    "IDR",
				Status:      "AVAILABLE",
			},
		},
	}))
	server := NewServerWithSeatMap(bookingService, seatMapService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/showtimes/"+showtimeID+"/seats", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if want := `"price_amount":"50000.00"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
	if want := `"status":"AVAILABLE"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerGetShowtimeSeatsReturnsNotFound(t *testing.T) {
	showtimeID := "10000000-0000-4000-8000-000000000002"
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(nil))
	server := NewServerWithSeatMap(bookingService, seatMapService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/showtimes/"+showtimeID+"/seats", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if want := `"code":"SHOWTIME_NOT_FOUND"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerGetShowtimeSeatsReturnsValidationErrorForInvalidShowtimeID(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(nil))
	server := NewServerWithSeatMap(bookingService, seatMapService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/showtimes/not-a-uuid/seats", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if want := `"code":"INVALID_REQUEST"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerListMovies(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(nil))
	movieCatalogService := catalog.NewService(catalog.NewMemoryRepository([]catalog.Movie{
		{
			ID:              "10000000-0000-4000-8000-000000000001",
			Title:           "First Movie",
			DurationMinutes: 120,
			CreatedAt:       time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC),
		},
	}))
	server := NewServerWithMovieCatalog(bookingService, seatMapService, movieCatalogService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/movies?limit=1", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if want := `"title":"First Movie"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerListMoviesRejectsInvalidLimit(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(nil))
	movieCatalogService := catalog.NewService(catalog.NewMemoryRepository(nil))
	server := NewServerWithMovieCatalog(bookingService, seatMapService, movieCatalogService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/movies?limit=101", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if want := `"code":"INVALID_REQUEST"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerListMoviesRejectsInvalidCursor(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(nil))
	movieCatalogService := catalog.NewService(catalog.NewMemoryRepository(nil))
	server := NewServerWithMovieCatalog(bookingService, seatMapService, movieCatalogService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/v1/movies?cursor=invalid", nil)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if want := `"code":"INVALID_REQUEST"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerListMovieShowtimes(t *testing.T) {
	movieID := "10000000-0000-4000-8000-000000000001"
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(nil))
	movieCatalogService := catalog.NewService(catalog.NewMemoryRepository(nil))
	showtimeService := scheduling.NewService(scheduling.NewMemoryRepository(map[string][]scheduling.Showtime{
		movieID: {
			{
				ID:         "20000000-0000-4000-8000-000000000001",
				StudioID:   "30000000-0000-4000-8000-000000000001",
				StudioName: "Studio 1",
				CinemaID:   "40000000-0000-4000-8000-000000000001",
				CinemaName: "Central Cinema",
				CinemaCity: "Jakarta",
				StartsAt:   time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC),
				EndsAt:     time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC),
				BasePrice:  "50000.00",
				Currency:   "IDR",
			},
		},
	}))
	server := NewServerWithPublicCatalog(bookingService, seatMapService, movieCatalogService, showtimeService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/movies/"+movieID+"/showtimes?date=2026-08-26",
		nil,
	)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if want := `"cinema_name":"Central Cinema"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
	if want := `"base_price":"50000.00"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerListMovieShowtimesRejectsInvalidDate(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(nil))
	movieCatalogService := catalog.NewService(catalog.NewMemoryRepository(nil))
	showtimeService := scheduling.NewService(scheduling.NewMemoryRepository(nil))
	server := NewServerWithPublicCatalog(bookingService, seatMapService, movieCatalogService, showtimeService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/movies/10000000-0000-4000-8000-000000000001/showtimes?date=invalid",
		nil,
	)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
	if want := `"code":"INVALID_REQUEST"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerListMovieShowtimesReturnsNotFound(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	seatMapService := seatinventory.NewService(seatinventory.NewMemoryRepository(nil))
	movieCatalogService := catalog.NewService(catalog.NewMemoryRepository(nil))
	showtimeService := scheduling.NewService(scheduling.NewMemoryRepository(nil))
	server := NewServerWithPublicCatalog(bookingService, seatMapService, movieCatalogService, showtimeService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodGet,
		"/v1/movies/10000000-0000-4000-8000-000000000002/showtimes?date=2026-08-26",
		nil,
	)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if want := `"code":"MOVIE_NOT_FOUND"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerCreateFakePayment(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, func() time.Time { return now })
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		func() time.Time { return now },
	)
	session, err := authenticationService.Register(context.Background(), auth.RegisterInput{
		Email:       "customer@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Customer",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	paymentService := payments.NewService(payments.NewMemoryRepository([]payments.Order{{
		ID:        "10000000-0000-4000-8000-000000000001",
		UserID:    session.User.ID,
		Status:    payments.OrderPendingPayment,
		ExpiresAt: now.Add(time.Minute),
		Items: []payments.OrderItem{{
			ID:          "20000000-0000-4000-8000-000000000001",
			SeatID:      "30000000-0000-4000-8000-000000000001",
			PriceAmount: "50000.00",
			Currency:    "IDR",
		}},
	}}), func() time.Time { return now })
	server := NewServerWithAllFeatures(
		bookingService,
		nil,
		nil,
		nil,
		paymentService,
		authenticationService,
		"bootstrap-token",
	)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/orders/10000000-0000-4000-8000-000000000001/payment-intents",
		nil,
	)
	request.Header.Set("Authorization", "Bearer "+session.AccessToken)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if want := `"status":"SUCCEEDED"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

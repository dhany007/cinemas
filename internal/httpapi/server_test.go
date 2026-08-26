package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/catalog"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/citradigital/cinemas/internal/scheduling"
	"github.com/citradigital/cinemas/internal/seatinventory"
)

func TestServerCreateOrderHold(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	service := booking.NewService(
		booking.NewMemoryRepository([]booking.Seat{{ID: "seat-a", ShowtimeID: "showtime-1", Status: booking.SeatAvailable}}),
		10*time.Minute,
		func() time.Time { return now },
	)
	server := NewServer(service)
	recorder := httptest.NewRecorder()
	requestBody := `{"user_id":"user-1","showtime_id":"showtime-1","seat_ids":["seat-a"]}`
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/orders",
		bytes.NewBufferString(requestBody),
	)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "checkout-1")

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
	paymentService := payments.NewService(payments.NewMemoryRepository([]payments.Order{{
		ID:        "10000000-0000-4000-8000-000000000001",
		Status:    payments.OrderPendingPayment,
		ExpiresAt: now.Add(time.Minute),
		Items: []payments.OrderItem{{
			ID:          "20000000-0000-4000-8000-000000000001",
			SeatID:      "30000000-0000-4000-8000-000000000001",
			PriceAmount: "50000.00",
			Currency:    "IDR",
		}},
	}}), func() time.Time { return now })
	server := NewServerWithFakePayments(bookingService, paymentService)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(
		http.MethodPost,
		"/v1/orders/10000000-0000-4000-8000-000000000001/payment-intents",
		nil,
	)

	server.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusCreated, recorder.Body.String())
	}
	if want := `"status":"SUCCEEDED"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
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

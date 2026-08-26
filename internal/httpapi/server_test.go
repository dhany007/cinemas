package httpapi

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
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
	request := httptest.NewRequest(http.MethodPost, "/v1/orders", bytes.NewBufferString(`{"user_id":"user-1","showtime_id":"showtime-1","seat_ids":["seat-a"]}`))
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

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

	"github.com/citradigital/cinemas/internal/admin"
	"github.com/citradigital/cinemas/internal/auth"
	"github.com/citradigital/cinemas/internal/booking"
	"github.com/citradigital/cinemas/internal/catalog"
	"github.com/citradigital/cinemas/internal/payments"
	"github.com/citradigital/cinemas/internal/scheduling"
	"github.com/citradigital/cinemas/internal/seatinventory"
	"github.com/labstack/echo/v4"
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

func TestServerReturnsOnlyOwnedOrdersAndAllowsCancellation(t *testing.T) {
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)
	bookingService := booking.NewService(
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
	owner, err := authenticationService.Register(context.Background(), auth.RegisterInput{
		Email: "order-owner@example.com", Password: "correct horse battery staple", DisplayName: "Owner",
	})
	if err != nil {
		t.Fatalf("Register() owner error = %v", err)
	}
	other, err := authenticationService.Register(context.Background(), auth.RegisterInput{
		Email: "other-customer@example.com", Password: "correct horse battery staple", DisplayName: "Other",
	})
	if err != nil {
		t.Fatalf("Register() other error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	createRequest := httptest.NewRequest(
		http.MethodPost,
		"/v1/orders",
		strings.NewReader(`{"showtime_id":"showtime-1","seat_ids":["seat-a"]}`),
	)
	createRequest.Header.Set(echo.HeaderAuthorization, "Bearer "+owner.AccessToken)
	createRequest.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	createRequest.Header.Set("Idempotency-Key", "order-lifecycle-1")
	create := httptest.NewRecorder()
	server.ServeHTTP(create, createRequest)
	if create.Code != http.StatusCreated {
		t.Fatalf("create order status = %d, body = %s", create.Code, create.Body.String())
	}
	var order struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(create.Body.Bytes(), &order); err != nil {
		t.Fatalf("unmarshal created order: %v", err)
	}

	get := serveAdminCinemaRequest(server, http.MethodGet, "/v1/orders/"+order.ID, "", owner.AccessToken)
	if get.Code != http.StatusOK {
		t.Fatalf("get order status = %d, body = %s", get.Code, get.Body.String())
	}
	if want := `"status":"PENDING_PAYMENT"`; !bytes.Contains(get.Body.Bytes(), []byte(want)) {
		t.Fatalf("get order body = %s, want %s", get.Body.String(), want)
	}
	history := serveAdminCinemaRequest(server, http.MethodGet, "/v1/orders", "", owner.AccessToken)
	if history.Code != http.StatusOK {
		t.Fatalf("list orders status = %d, body = %s", history.Code, history.Body.String())
	}
	foreign := serveAdminCinemaRequest(server, http.MethodGet, "/v1/orders/"+order.ID, "", other.AccessToken)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("foreign get order status = %d, body = %s", foreign.Code, foreign.Body.String())
	}
	cancel := serveAdminCinemaRequest(server, http.MethodPost, "/v1/orders/"+order.ID+"/cancel", "", owner.AccessToken)
	if cancel.Code != http.StatusOK {
		t.Fatalf("cancel order status = %d, body = %s", cancel.Code, cancel.Body.String())
	}
	if want := `"status":"CANCELLED"`; !bytes.Contains(cancel.Body.Bytes(), []byte(want)) {
		t.Fatalf("cancel order body = %s, want %s", cancel.Body.String(), want)
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

func TestServerAdminCinemaCRUD(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	adminSession, err := authenticationService.RegisterAdmin(context.Background(), auth.RegisterInput{
		Email:       "admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Administrator",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	adminRepository := admin.NewMemoryRepository()
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(adminRepository))

	createRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/cinemas",
		`{"name":"Central Cinema","address":"Jl. Example 1","city":"Jakarta"}`,
		adminSession.AccessToken,
	)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf(
			"create status = %d, want %d; body = %s",
			createRecorder.Code,
			http.StatusCreated,
			createRecorder.Body.String(),
		)
	}

	var created cinemaResponse
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal create response: %v", err)
	}
	if !isUUID(created.ID) {
		t.Fatalf("created cinema ID = %q, want UUID", created.ID)
	}

	listRecorder := serveAdminCinemaRequest(server, http.MethodGet, "/v1/admin/cinemas", "", adminSession.AccessToken)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body = %s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	if want := `"cinemas":[{"id":"` + created.ID + `"`; !bytes.Contains(listRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("list body = %s, want %s", listRecorder.Body.String(), want)
	}

	getRecorder := serveAdminCinemaRequest(
		server,
		http.MethodGet,
		"/v1/admin/cinemas/"+created.ID,
		"",
		adminSession.AccessToken,
	)
	if getRecorder.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body = %s", getRecorder.Code, http.StatusOK, getRecorder.Body.String())
	}

	updateRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPatch,
		"/v1/admin/cinemas/"+created.ID,
		`{"name":"Central Cinema Updated","address":"Jl. Example 2","city":"Bandung"}`,
		adminSession.AccessToken,
	)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, want %d; body = %s", updateRecorder.Code, http.StatusOK, updateRecorder.Body.String())
	}
	if want := `"name":"Central Cinema Updated"`; !bytes.Contains(updateRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("update body = %s, want %s", updateRecorder.Body.String(), want)
	}

	deleteRecorder := serveAdminCinemaRequest(
		server,
		http.MethodDelete,
		"/v1/admin/cinemas/"+created.ID,
		"",
		adminSession.AccessToken,
	)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf(
			"delete status = %d, want %d; body = %s",
			deleteRecorder.Code,
			http.StatusNoContent,
			deleteRecorder.Body.String(),
		)
	}

	notFoundRecorder := serveAdminCinemaRequest(
		server,
		http.MethodGet,
		"/v1/admin/cinemas/"+created.ID,
		"",
		adminSession.AccessToken,
	)
	if notFoundRecorder.Code != http.StatusNotFound {
		t.Fatalf("not found status = %d, want %d", notFoundRecorder.Code, http.StatusNotFound)
	}
	if want := `"code":"CINEMA_NOT_FOUND"`; !bytes.Contains(notFoundRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("not found body = %s, want %s", notFoundRecorder.Body.String(), want)
	}

	if got := adminRepository.AuditEvents(); len(got) != 3 {
		t.Fatalf("audit events = %#v, want create, update, and delete", got)
	}
}

func TestServerRejectsCustomerCinemaManagement(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	customerSession, err := authenticationService.Register(context.Background(), auth.RegisterInput{
		Email:       "customer@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Customer",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))

	recorder := serveAdminCinemaRequest(
		server,
		http.MethodGet,
		"/v1/admin/cinemas",
		"",
		customerSession.AccessToken,
	)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if want := `"code":"FORBIDDEN"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerAdminStudioCRUD(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	//nolint:lll // Test authentication setup is clearer inline.
	authenticationService := auth.NewService(auth.NewMemoryRepository(), []byte("01234567890123456789012345678901"), time.Hour, time.Now)
	adminSession, err := authenticationService.RegisterAdmin(context.Background(), auth.RegisterInput{
		Email: "studio-admin@example.com", Password: "correct horse battery staple", DisplayName: "Administrator",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	adminRepository := admin.NewMemoryRepository()
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(adminRepository))
	//nolint:lll // The request fixture is intentionally visible.
	createCinema := serveAdminCinemaRequest(server, http.MethodPost, "/v1/admin/cinemas", `{"name":"Central Cinema","address":"Jl. Example 1","city":"Jakarta"}`, adminSession.AccessToken)
	var cinema cinemaResponse
	if err := json.Unmarshal(createCinema.Body.Bytes(), &cinema); err != nil {
		t.Fatalf("unmarshal cinema: %v", err)
	}
	//nolint:lll // The request fixture is intentionally visible.
	createStudio := serveAdminCinemaRequest(server, http.MethodPost, "/v1/admin/studios", `{"cinema_id":"`+cinema.ID+`","name":"Studio 1"}`, adminSession.AccessToken)
	if createStudio.Code != http.StatusCreated {
		t.Fatalf("create studio status = %d, body = %s", createStudio.Code, createStudio.Body.String())
	}
	var studio struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createStudio.Body.Bytes(), &studio); err != nil {
		t.Fatalf("unmarshal studio: %v", err)
	}
	listStudio := serveAdminCinemaRequest(server, http.MethodGet, "/v1/admin/studios", "", adminSession.AccessToken)
	if listStudio.Code != http.StatusOK {
		t.Fatalf("list studio status = %d, body = %s", listStudio.Code, listStudio.Body.String())
	}
	//nolint:lll // The request fixture is intentionally visible.
	updateStudio := serveAdminCinemaRequest(server, http.MethodPatch, "/v1/admin/studios/"+studio.ID, `{"cinema_id":"`+cinema.ID+`","name":"Studio 2"}`, adminSession.AccessToken)
	if updateStudio.Code != http.StatusOK {
		t.Fatalf("update studio status = %d, body = %s", updateStudio.Code, updateStudio.Body.String())
	}
	//nolint:lll // The path is a direct composition of the created ID.
	deleteStudio := serveAdminCinemaRequest(server, http.MethodDelete, "/v1/admin/studios/"+studio.ID, "", adminSession.AccessToken)
	if deleteStudio.Code != http.StatusNoContent {
		t.Fatalf("delete studio status = %d, body = %s", deleteStudio.Code, deleteStudio.Body.String())
	}
}

func TestServerAdminSeatLayoutCRUD(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	adminSession, err := authenticationService.RegisterAdmin(context.Background(), auth.RegisterInput{
		Email:       "seat-admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Administrator",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))

	cinema := createAdminCinema(t, server, adminSession.AccessToken)
	studio := createAdminStudio(t, server, cinema.ID, adminSession.AccessToken)
	createRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/seats",
		`{"studio_id":"`+studio.ID+`","row_label":"A","seat_number":"1","seat_type":"STANDARD"}`,
		adminSession.AccessToken,
	)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRecorder.Code, createRecorder.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created seat: %v", err)
	}
	if !isUUID(created.ID) {
		t.Fatalf("created seat ID = %q, want UUID", created.ID)
	}

	listRecorder := serveAdminCinemaRequest(server, http.MethodGet, "/v1/admin/seats", "", adminSession.AccessToken)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRecorder.Code, listRecorder.Body.String())
	}
	if want := `"seat_number":"1"`; !bytes.Contains(listRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("list body = %s, want %s", listRecorder.Body.String(), want)
	}

	updateRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPatch,
		"/v1/admin/seats/"+created.ID,
		`{"studio_id":"`+studio.ID+`","row_label":"A","seat_number":"2","seat_type":"PREMIUM"}`,
		adminSession.AccessToken,
	)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	if want := `"seat_type":"PREMIUM"`; !bytes.Contains(updateRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("update body = %s, want %s", updateRecorder.Body.String(), want)
	}

	deleteRecorder := serveAdminCinemaRequest(
		server,
		http.MethodDelete,
		"/v1/admin/seats/"+created.ID,
		"",
		adminSession.AccessToken,
	)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestServerRejectsCustomerSeatLayoutManagement(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	customerSession, err := authenticationService.Register(context.Background(), auth.RegisterInput{
		Email:       "seat-customer@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Customer",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))

	recorder := serveAdminCinemaRequest(server, http.MethodGet, "/v1/admin/seats", "", customerSession.AccessToken)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if want := `"code":"FORBIDDEN"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerRejectsDuplicateSeatLayoutPosition(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	adminSession, err := authenticationService.RegisterAdmin(context.Background(), auth.RegisterInput{
		Email:       "seat-conflict-admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Administrator",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))
	cinema := createAdminCinema(t, server, adminSession.AccessToken)
	studio := createAdminStudio(t, server, cinema.ID, adminSession.AccessToken)
	body := `{"studio_id":"` + studio.ID + `","row_label":"A","seat_number":"1","seat_type":"STANDARD"}`
	first := serveAdminCinemaRequest(server, http.MethodPost, "/v1/admin/seats", body, adminSession.AccessToken)
	if first.Code != http.StatusCreated {
		t.Fatalf("first create status = %d, body = %s", first.Code, first.Body.String())
	}

	duplicate := serveAdminCinemaRequest(server, http.MethodPost, "/v1/admin/seats", body, adminSession.AccessToken)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status = %d, want %d; body = %s", duplicate.Code, http.StatusConflict, duplicate.Body.String())
	}
	if want := `"code":"SEAT_ALREADY_EXISTS"`; !bytes.Contains(duplicate.Body.Bytes(), []byte(want)) {
		t.Fatalf("duplicate body = %s, want %s", duplicate.Body.String(), want)
	}
}

func TestServerAdminMovieCRUD(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	adminSession, err := authenticationService.RegisterAdmin(context.Background(), auth.RegisterInput{
		Email:       "movie-admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Administrator",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))
	body := `{"title":"Example Movie","duration_minutes":120,"rating":"PG-13",` +
		`"synopsis":"A thrilling adventure.","poster_url":"https://example.com/poster.jpg",` +
		`"release_date":"2026-08-26"}`
	createRecorder := serveAdminCinemaRequest(server, http.MethodPost, "/v1/admin/movies", body, adminSession.AccessToken)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRecorder.Code, createRecorder.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created movie: %v", err)
	}
	if !isUUID(created.ID) {
		t.Fatalf("created movie ID = %q, want UUID", created.ID)
	}

	listRecorder := serveAdminCinemaRequest(server, http.MethodGet, "/v1/admin/movies", "", adminSession.AccessToken)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRecorder.Code, listRecorder.Body.String())
	}
	if want := `"title":"Example Movie"`; !bytes.Contains(listRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("list body = %s, want %s", listRecorder.Body.String(), want)
	}

	updateBody := `{"title":"Example Movie: Director's Cut","duration_minutes":130,` +
		`"rating":"PG-13","synopsis":"Updated adventure.",` +
		`"poster_url":"https://example.com/poster.jpg","release_date":"2026-08-27"}`
	updateRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPatch,
		"/v1/admin/movies/"+created.ID,
		updateBody,
		adminSession.AccessToken,
	)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	if want := `"duration_minutes":130`; !bytes.Contains(updateRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("update body = %s, want %s", updateRecorder.Body.String(), want)
	}

	deleteRecorder := serveAdminCinemaRequest(
		server,
		http.MethodDelete,
		"/v1/admin/movies/"+created.ID,
		"",
		adminSession.AccessToken,
	)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestServerRejectsCustomerMovieManagement(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	customerSession, err := authenticationService.Register(context.Background(), auth.RegisterInput{
		Email:       "movie-customer@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Customer",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))

	recorder := serveAdminCinemaRequest(server, http.MethodGet, "/v1/admin/movies", "", customerSession.AccessToken)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if want := `"code":"FORBIDDEN"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerRejectsInvalidMovieMetadata(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	adminSession, err := authenticationService.RegisterAdmin(context.Background(), auth.RegisterInput{
		Email:       "invalid-movie-admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Administrator",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))

	recorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/movies",
		`{"title":"Example Movie","duration_minutes":0}`,
		adminSession.AccessToken,
	)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if want := `"code":"INVALID_REQUEST"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerAdminShowtimeCRUD(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	adminSession, err := authenticationService.RegisterAdmin(context.Background(), auth.RegisterInput{
		Email:       "showtime-admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Administrator",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))
	cinema := createAdminCinema(t, server, adminSession.AccessToken)
	studio := createAdminStudio(t, server, cinema.ID, adminSession.AccessToken)
	seatRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/seats",
		`{"studio_id":"`+studio.ID+`","row_label":"A","seat_number":"1","seat_type":"STANDARD"}`,
		adminSession.AccessToken,
	)
	if seatRecorder.Code != http.StatusCreated {
		t.Fatalf("seat create status = %d, body = %s", seatRecorder.Code, seatRecorder.Body.String())
	}
	var seat struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(seatRecorder.Body.Bytes(), &seat); err != nil {
		t.Fatalf("unmarshal created seat: %v", err)
	}
	movie := createAdminMovie(t, server, adminSession.AccessToken)
	body := `{"movie_id":"` + movie.ID + `","studio_id":"` + studio.ID + `",` +
		`"starts_at":"2026-08-26T10:00:00Z","ends_at":"2026-08-26T12:00:00Z",` +
		`"base_price":"50000.00","currency":"IDR"}`
	createRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/showtimes",
		body,
		adminSession.AccessToken,
	)
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createRecorder.Code, createRecorder.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createRecorder.Body.Bytes(), &created); err != nil {
		t.Fatalf("unmarshal created showtime: %v", err)
	}
	if !isUUID(created.ID) {
		t.Fatalf("created showtime ID = %q, want UUID", created.ID)
	}
	blockedSeatBody := `{"studio_id":"` + studio.ID + `","row_label":"A","seat_number":"2","seat_type":"STANDARD"}`
	blockedCreate := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/seats",
		blockedSeatBody,
		adminSession.AccessToken,
	)
	if blockedCreate.Code != http.StatusConflict {
		t.Fatalf("seat create after showtime status = %d, body = %s", blockedCreate.Code, blockedCreate.Body.String())
	}
	if want := `"code":"SEAT_LAYOUT_IN_USE"`; !bytes.Contains(blockedCreate.Body.Bytes(), []byte(want)) {
		t.Fatalf("seat create after showtime body = %s, want %s", blockedCreate.Body.String(), want)
	}
	blockedUpdate := serveAdminCinemaRequest(
		server,
		http.MethodPatch,
		"/v1/admin/seats/"+seat.ID,
		blockedSeatBody,
		adminSession.AccessToken,
	)
	if blockedUpdate.Code != http.StatusConflict {
		t.Fatalf("seat update after showtime status = %d, body = %s", blockedUpdate.Code, blockedUpdate.Body.String())
	}
	blockedDelete := serveAdminCinemaRequest(
		server,
		http.MethodDelete,
		"/v1/admin/seats/"+seat.ID,
		"",
		adminSession.AccessToken,
	)
	if blockedDelete.Code != http.StatusConflict {
		t.Fatalf("seat delete after showtime status = %d, body = %s", blockedDelete.Code, blockedDelete.Body.String())
	}
	overlapBody := `{"movie_id":"` + movie.ID + `","studio_id":"` + studio.ID + `",` +
		`"starts_at":"2026-08-26T11:00:00Z","ends_at":"2026-08-26T13:00:00Z",` +
		`"base_price":"50000.00","currency":"IDR"}`
	overlapRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/showtimes",
		overlapBody,
		adminSession.AccessToken,
	)
	if overlapRecorder.Code != http.StatusConflict {
		t.Fatalf("overlap status = %d, body = %s", overlapRecorder.Code, overlapRecorder.Body.String())
	}
	if want := `"code":"SHOWTIME_OVERLAP"`; !bytes.Contains(overlapRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("overlap body = %s, want %s", overlapRecorder.Body.String(), want)
	}

	listRecorder := serveAdminCinemaRequest(server, http.MethodGet, "/v1/admin/showtimes", "", adminSession.AccessToken)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listRecorder.Code, listRecorder.Body.String())
	}
	if want := `"base_price":"50000.00"`; !bytes.Contains(listRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("list body = %s, want %s", listRecorder.Body.String(), want)
	}

	updateBody := `{"movie_id":"` + movie.ID + `","studio_id":"` + studio.ID + `",` +
		`"starts_at":"2026-08-26T13:00:00Z","ends_at":"2026-08-26T15:00:00Z",` +
		`"base_price":"55000.00","currency":"IDR"}`
	updateRecorder := serveAdminCinemaRequest(
		server,
		http.MethodPatch,
		"/v1/admin/showtimes/"+created.ID,
		updateBody,
		adminSession.AccessToken,
	)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status = %d, body = %s", updateRecorder.Code, updateRecorder.Body.String())
	}
	if want := `"starts_at":"2026-08-26T13:00:00Z"`; !bytes.Contains(updateRecorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("update body = %s, want %s", updateRecorder.Body.String(), want)
	}

	deleteRecorder := serveAdminCinemaRequest(
		server,
		http.MethodDelete,
		"/v1/admin/showtimes/"+created.ID,
		"",
		adminSession.AccessToken,
	)
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body = %s", deleteRecorder.Code, deleteRecorder.Body.String())
	}
}

func TestServerRejectsCustomerShowtimeManagement(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	customerSession, err := authenticationService.Register(context.Background(), auth.RegisterInput{
		Email:       "showtime-customer@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Customer",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))

	recorder := serveAdminCinemaRequest(server, http.MethodGet, "/v1/admin/showtimes", "", customerSession.AccessToken)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if want := `"code":"FORBIDDEN"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func TestServerRejectsInvalidShowtimeMetadata(t *testing.T) {
	bookingService := booking.NewService(booking.NewMemoryRepository(nil), 10*time.Minute, time.Now)
	authenticationService := auth.NewService(
		auth.NewMemoryRepository(),
		[]byte("01234567890123456789012345678901"),
		time.Hour,
		time.Now,
	)
	adminSession, err := authenticationService.RegisterAdmin(context.Background(), auth.RegisterInput{
		Email:       "invalid-showtime-admin@example.com",
		Password:    "correct horse battery staple",
		DisplayName: "Administrator",
	})
	if err != nil {
		t.Fatalf("RegisterAdmin() error = %v", err)
	}
	server := NewServerWithAuth(bookingService, authenticationService, "bootstrap-token")
	server.EnableAdminCinemaRoutes(authenticationService, admin.NewService(admin.NewMemoryRepository()))
	body := `{"movie_id":"10000000-0000-4000-8000-000000000001",` +
		`"studio_id":"20000000-0000-4000-8000-000000000001",` +
		`"starts_at":"2026-08-26T12:00:00Z","ends_at":"2026-08-26T10:00:00Z",` +
		`"base_price":"50000.00","currency":"IDR"}`
	recorder := serveAdminCinemaRequest(server, http.MethodPost, "/v1/admin/showtimes", body, adminSession.AccessToken)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
	}
	if want := `"code":"INVALID_REQUEST"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
}

func createAdminMovie(t *testing.T, server *Server, accessToken string) movieResponse {
	t.Helper()
	recorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/movies",
		`{"title":"Example Movie","duration_minutes":120}`,
		accessToken,
	)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create movie status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var movie movieResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &movie); err != nil {
		t.Fatalf("unmarshal movie: %v", err)
	}
	return movie
}

func createAdminCinema(t *testing.T, server *Server, accessToken string) cinemaResponse {
	t.Helper()
	recorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/cinemas",
		`{"name":"Central Cinema","address":"Jl. Example 1","city":"Jakarta"}`,
		accessToken,
	)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create cinema status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var cinema cinemaResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &cinema); err != nil {
		t.Fatalf("unmarshal cinema: %v", err)
	}
	return cinema
}

func createAdminStudio(t *testing.T, server *Server, cinemaID, accessToken string) studioResponse {
	t.Helper()
	recorder := serveAdminCinemaRequest(
		server,
		http.MethodPost,
		"/v1/admin/studios",
		`{"cinema_id":"`+cinemaID+`","name":"Studio 1"}`,
		accessToken,
	)
	if recorder.Code != http.StatusCreated {
		t.Fatalf("create studio status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	var studio studioResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &studio); err != nil {
		t.Fatalf("unmarshal studio: %v", err)
	}
	return studio
}

func serveAdminCinemaRequest(
	server *Server,
	method string,
	path string,
	body string,
	accessToken string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set(echo.HeaderAuthorization, "Bearer "+accessToken)
	if body != "" {
		request.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	}
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)
	return recorder
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

func TestServerCreatesPendingIntentAndAcceptsVerifiedWebhook(t *testing.T) {
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
	paymentRepository := payments.NewMemoryRepository([]payments.Order{{
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
	}})
	provider := payments.NewFakeProvider("test-webhook-secret-must-be-at-least-32-bytes", 5*time.Minute)
	paymentService := payments.NewService(paymentRepository, provider, func() time.Time { return now })
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
	if want := `"status":"PENDING"`; !bytes.Contains(recorder.Body.Bytes(), []byte(want)) {
		t.Fatalf("response body = %s, want %s", recorder.Body.String(), want)
	}
	if order, ok := paymentRepository.Order("10000000-0000-4000-8000-000000000001"); !ok || order.Status != payments.OrderPendingPayment {
		t.Fatalf("order after intent = %#v, want pending", order)
	}

	payload := provider.SuccessPayload("evt-http-payment-1", "fake-10000000-0000-4000-8000-000000000001", now)
	webhook := httptest.NewRequest(http.MethodPost, "/v1/webhooks/payments/FAKE", bytes.NewReader(payload))
	webhook.Header.Set("X-Payment-Timestamp", now.Format(time.RFC3339))
	webhook.Header.Set("X-Payment-Signature", provider.Sign(payload, now))
	webhookRecorder := httptest.NewRecorder()

	server.ServeHTTP(webhookRecorder, webhook)

	if webhookRecorder.Code != http.StatusNoContent {
		t.Fatalf("webhook status = %d, want %d; body = %s", webhookRecorder.Code, http.StatusNoContent, webhookRecorder.Body.String())
	}
	if order, ok := paymentRepository.Order("10000000-0000-4000-8000-000000000001"); !ok || order.Status != payments.OrderPaid {
		t.Fatalf("order after webhook = %#v, want paid", order)
	}
}

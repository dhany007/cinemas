package postgres

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/citradigital/cinemas/internal/booking"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestBookingRepositoryPostgreSQLConcurrency(t *testing.T) {
	databaseURL := os.Getenv("CINEMAS_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("CINEMAS_TEST_DATABASE_URL is required for PostgreSQL integration tests")
	}
	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("open PostgreSQL test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	repository := NewBookingRepository(pool)
	now := time.Date(2026, time.August, 26, 10, 0, 0, 0, time.UTC)

	t.Run("duplicate requests return one order", func(t *testing.T) {
		userID, showtimeID, seatIDs := seedBookingTestData(t, pool, 1)
		order := booking.Order{ID: databaseUUID(t, pool), UserID: userID, ShowtimeID: showtimeID, IdempotencyKey: "duplicate", Status: booking.OrderPendingPayment, ExpiresAt: now.Add(time.Minute), Items: []booking.OrderItem{{SeatID: seatIDs[0]}}}
		first, err := repository.CreateHold(context.Background(), order, now)
		if err != nil {
			t.Fatalf("first CreateHold() error = %v", err)
		}
		second, err := repository.CreateHold(context.Background(), order, now)
		if err != nil {
			t.Fatalf("second CreateHold() error = %v", err)
		}
		if second.ID != first.ID {
			t.Fatalf("duplicate order ID = %q, want %q", second.ID, first.ID)
		}
	})

	t.Run("competing selections produce one winner", func(t *testing.T) {
		firstUserID, showtimeID, seatIDs := seedBookingTestData(t, pool, 1)
		secondUserID := databaseUUID(t, pool)
		if _, err := pool.Exec(context.Background(), `INSERT INTO users (id, email, display_name) VALUES ($1, $2, 'Customer')`, secondUserID, secondUserID+"@example.test"); err != nil {
			t.Fatalf("insert second user: %v", err)
		}
		orders := []booking.Order{
			{ID: databaseUUID(t, pool), UserID: firstUserID, ShowtimeID: showtimeID, IdempotencyKey: "first", Status: booking.OrderPendingPayment, ExpiresAt: now.Add(time.Minute), Items: []booking.OrderItem{{SeatID: seatIDs[0]}}},
			{ID: databaseUUID(t, pool), UserID: secondUserID, ShowtimeID: showtimeID, IdempotencyKey: "second", Status: booking.OrderPendingPayment, ExpiresAt: now.Add(time.Minute), Items: []booking.OrderItem{{SeatID: seatIDs[0]}}},
		}
		var successes int
		var mu sync.Mutex
		var wait sync.WaitGroup
		for _, order := range orders {
			order := order
			wait.Add(1)
			go func() {
				defer wait.Done()
				if _, err := repository.CreateHold(context.Background(), order, now); err == nil {
					mu.Lock()
					successes++
					mu.Unlock()
				}
			}()
		}
		wait.Wait()
		if successes != 1 {
			t.Fatalf("competing holds successes = %d, want 1", successes)
		}
	})

	t.Run("expiry marks order and releases inventory", func(t *testing.T) {
		userID, showtimeID, seatIDs := seedBookingTestData(t, pool, 1)
		order := booking.Order{ID: databaseUUID(t, pool), UserID: userID, ShowtimeID: showtimeID, IdempotencyKey: "expiry", Status: booking.OrderPendingPayment, ExpiresAt: now.Add(time.Minute), Items: []booking.OrderItem{{SeatID: seatIDs[0]}}}
		if _, err := repository.CreateHold(context.Background(), order, now); err != nil {
			t.Fatalf("CreateHold() error = %v", err)
		}
		expired, err := repository.ExpirePendingHolds(context.Background(), order.ExpiresAt, 10)
		if err != nil {
			t.Fatalf("ExpirePendingHolds() error = %v", err)
		}
		if expired < 1 {
			t.Fatalf("expired orders = %d, want at least 1", expired)
		}
		stored, err := repository.FindOrder(context.Background(), order.ID, userID)
		if err != nil {
			t.Fatalf("FindOrder() error = %v", err)
		}
		if stored.Status != booking.OrderExpired {
			t.Fatalf("order status = %q, want %q", stored.Status, booking.OrderExpired)
		}
		var status string
		if err := pool.QueryRow(context.Background(), `SELECT status FROM showtime_seats WHERE id=$1`, seatIDs[0]).Scan(&status); err != nil {
			t.Fatalf("query released seat: %v", err)
		}
		if status != string(booking.SeatAvailable) {
			t.Fatalf("seat status = %q, want AVAILABLE", status)
		}
	})
}

func seedBookingTestData(t *testing.T, pool *pgxpool.Pool, seatCount int) (string, string, []string) {
	t.Helper()
	ctx := context.Background()
	userID, cinemaID, studioID, movieID, showtimeID := databaseUUID(t, pool), databaseUUID(t, pool), databaseUUID(t, pool), databaseUUID(t, pool), databaseUUID(t, pool)
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, $2, 'Customer')`, userID, userID+"@example.test"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO cinemas (id, name, address, city) VALUES ($1, 'Test Cinema', 'Test', 'Jakarta')`, cinemaID); err != nil {
		t.Fatalf("seed cinema: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO studios (id, cinema_id, name) VALUES ($1, $2, 'Studio')`, studioID, cinemaID); err != nil {
		t.Fatalf("seed studio: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO movies (id, title, duration_minutes) VALUES ($1, 'Movie', 120)`, movieID); err != nil {
		t.Fatalf("seed movie: %v", err)
	}
	seatIDs := make([]string, seatCount)
	for i := range seatIDs {
		physicalID, showtimeSeatID := databaseUUID(t, pool), databaseUUID(t, pool)
		seatIDs[i] = showtimeSeatID
		if _, err := pool.Exec(ctx, `INSERT INTO seats (id, studio_id, row_label, seat_number, seat_type) VALUES ($1, $2, 'A', $3, 'STANDARD')`, physicalID, studioID, string(rune('1'+i))); err != nil {
			t.Fatalf("seed seat: %v", err)
		}
		if i == seatCount-1 {
			if _, err := pool.Exec(ctx, `INSERT INTO showtimes (id, movie_id, studio_id, starts_at, ends_at, base_price, currency) VALUES ($1, $2, $3, '2026-08-27T10:00:00Z', '2026-08-27T12:00:00Z', 50000, 'IDR')`, showtimeID, movieID, studioID); err != nil {
				t.Fatalf("seed showtime: %v", err)
			}
		}
		if _, err := pool.Exec(ctx, `INSERT INTO showtime_seats (id, showtime_id, seat_id, price_amount, currency) VALUES ($1, $2, $3, 50000, 'IDR')`, showtimeSeatID, showtimeID, physicalID); err != nil {
			t.Fatalf("seed showtime seat: %v", err)
		}
	}
	return userID, showtimeID, seatIDs
}

func databaseUUID(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatalf("generate UUID: %v", err)
	}
	return id
}

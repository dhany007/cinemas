package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/citradigital/cinemas/internal/tickets"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TicketsRepository provides owner-scoped ticket reads and durable delivery work.
type TicketsRepository struct {
	pool *pgxpool.Pool
}

func NewTicketsRepository(pool *pgxpool.Pool) *TicketsRepository {
	return &TicketsRepository{pool: pool}
}

func (r *TicketsRepository) ListOrderTickets(ctx context.Context, orderID, userID string) ([]tickets.Ticket, error) {
	rows, err := r.pool.Query(ctx, `
SELECT t.id::text, t.order_id::text, o.user_id::text, t.ticket_code, t.status
FROM tickets t
JOIN orders o ON o.id = t.order_id
WHERE t.order_id = $1 AND o.user_id = $2 AND o.status = 'PAID'
ORDER BY t.created_at, t.id`, orderID, userID)
	if err != nil {
		return nil, fmt.Errorf("query owner tickets: %w", err)
	}
	defer rows.Close()
	result, err := scanTickets(rows)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, tickets.ErrOrderNotFound
	}
	return result, nil
}

func (r *TicketsRepository) ClaimTicketDeliveries(
	ctx context.Context,
	now time.Time,
	limit int,
	lease time.Duration,
) ([]tickets.DeliveryEvent, error) {
	leaseUntil := now.Add(lease)
	rows, err := r.pool.Query(ctx, `
WITH candidates AS (
    SELECT id
    FROM outbox_events
    WHERE event_type = 'TICKET_DELIVERY_REQUESTED'
      AND status IN ('PENDING', 'PROCESSING')
      AND available_at <= $1
    ORDER BY available_at, id
    LIMIT $2
    FOR UPDATE SKIP LOCKED
)
UPDATE outbox_events AS event
SET status = 'PROCESSING', attempts = event.attempts + 1, available_at = $3
WHERE event.id IN (SELECT id FROM candidates)
RETURNING event.id, event.payload->>'order_id', event.attempts`, now, limit, leaseUntil)
	if err != nil {
		return nil, fmt.Errorf("claim ticket delivery events: %w", err)
	}
	defer rows.Close()
	events := make([]tickets.DeliveryEvent, 0)
	for rows.Next() {
		var event tickets.DeliveryEvent
		if err := rows.Scan(&event.ID, &event.OrderID, &event.Attempts); err != nil {
			return nil, fmt.Errorf("scan ticket delivery event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ticket delivery events: %w", err)
	}
	return events, nil
}

func (r *TicketsRepository) LoadDelivery(ctx context.Context, event tickets.DeliveryEvent) (tickets.Delivery, error) {
	var delivery tickets.Delivery
	err := r.pool.QueryRow(ctx, `
SELECT o.id::text, u.email
FROM orders o
JOIN users u ON u.id = o.user_id
WHERE o.id = $1 AND o.status = 'PAID'`, event.OrderID).Scan(&delivery.OrderID, &delivery.Email)
	if errors.Is(err, pgx.ErrNoRows) {
		return tickets.Delivery{}, tickets.ErrOrderNotFound
	}
	if err != nil {
		return tickets.Delivery{}, fmt.Errorf("load delivery recipient: %w", err)
	}
	rows, err := r.pool.Query(ctx, `
SELECT t.id::text, t.order_id::text, o.user_id::text, t.ticket_code, t.status
FROM tickets t
JOIN orders o ON o.id = t.order_id
WHERE t.order_id = $1
ORDER BY t.created_at, t.id`, event.OrderID)
	if err != nil {
		return tickets.Delivery{}, fmt.Errorf("load delivery tickets: %w", err)
	}
	defer rows.Close()
	delivery.Tickets, err = scanTickets(rows)
	if err != nil {
		return tickets.Delivery{}, err
	}
	if len(delivery.Tickets) == 0 {
		return tickets.Delivery{}, tickets.ErrOrderNotFound
	}
	return delivery, nil
}

func (r *TicketsRepository) CompleteTicketDelivery(ctx context.Context, eventID int64, now time.Time) error {
	command, err := r.pool.Exec(ctx, `
UPDATE outbox_events
SET status = 'COMPLETED', processed_at = $1
WHERE id = $2 AND event_type = 'TICKET_DELIVERY_REQUESTED' AND status = 'PROCESSING'`, now, eventID)
	if err != nil {
		return fmt.Errorf("complete ticket delivery event: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("complete ticket delivery event: event not claimed")
	}
	return nil
}

func (r *TicketsRepository) RetryTicketDelivery(ctx context.Context, eventID int64, now time.Time, delay time.Duration) error {
	command, err := r.pool.Exec(ctx, `
UPDATE outbox_events
SET status = 'PENDING', available_at = $1
WHERE id = $2 AND event_type = 'TICKET_DELIVERY_REQUESTED' AND status = 'PROCESSING'`, now.Add(delay), eventID)
	if err != nil {
		return fmt.Errorf("reschedule ticket delivery event: %w", err)
	}
	if command.RowsAffected() != 1 {
		return fmt.Errorf("reschedule ticket delivery event: event not claimed")
	}
	return nil
}

func (r *TicketsRepository) LookupAdminTicket(ctx context.Context, qrToken string) (tickets.AdminTicket, error) {
	ticket, err := findAdminTicket(ctx, r.pool, qrToken, false)
	if err != nil {
		return tickets.AdminTicket{}, err
	}
	return ticket, nil
}

func (r *TicketsRepository) CheckInTicket(
	ctx context.Context,
	qrToken string,
	adminUserID string,
	now time.Time,
) (tickets.AdminTicket, error) {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return tickets.AdminTicket{}, fmt.Errorf("begin ticket check-in: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	ticket, err := findAdminTicket(ctx, tx, qrToken, true)
	if err != nil {
		return tickets.AdminTicket{}, err
	}
	if ticket.Status == tickets.TicketUsed {
		return tickets.AdminTicket{}, tickets.ErrTicketAlreadyUsed
	}
	if ticket.Status != tickets.TicketIssued {
		return tickets.AdminTicket{}, tickets.ErrTicketNotFound
	}
	if _, err := tx.Exec(ctx, `
UPDATE tickets
SET status = 'USED', checked_in_at = $1, updated_at = now()
WHERE id = $2`, now, ticket.ID); err != nil {
		return tickets.AdminTicket{}, fmt.Errorf("mark ticket used: %w", err)
	}
	if _, err := tx.Exec(ctx, `
INSERT INTO audit_events (actor_user_id, entity_type, entity_id, action)
VALUES ($1, 'TICKET', $2, 'TICKET_CHECKED_IN')`, adminUserID, ticket.ID); err != nil {
		return tickets.AdminTicket{}, fmt.Errorf("record ticket check-in audit event: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return tickets.AdminTicket{}, fmt.Errorf("commit ticket check-in: %w", err)
	}
	ticket.Status = tickets.TicketUsed
	checkedInAt := now
	ticket.CheckedInAt = &checkedInAt
	return ticket, nil
}

func (r *TicketsRepository) ListExpiringHolds(ctx context.Context, now time.Time, limit int) ([]tickets.ExpiringHold, error) {
	rows, err := r.pool.Query(ctx, `
SELECT o.id::text, o.expires_at, COUNT(oi.id)
FROM orders o
JOIN order_items oi ON oi.order_id = o.id
WHERE o.status = 'PENDING_PAYMENT'
  AND o.expires_at > $1
  AND o.expires_at <= $1 + INTERVAL '15 minutes'
GROUP BY o.id, o.expires_at
ORDER BY o.expires_at, o.id
LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("query expiring holds: %w", err)
	}
	defer rows.Close()
	holds := make([]tickets.ExpiringHold, 0)
	for rows.Next() {
		var hold tickets.ExpiringHold
		if err := rows.Scan(&hold.OrderID, &hold.ExpiresAt, &hold.SeatCount); err != nil {
			return nil, fmt.Errorf("scan expiring hold: %w", err)
		}
		holds = append(holds, hold)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate expiring holds: %w", err)
	}
	return holds, nil
}

func (r *TicketsRepository) ListPaymentExceptions(ctx context.Context, limit int) ([]tickets.PaymentException, error) {
	rows, err := r.pool.Query(ctx, `
SELECT order_id::text, provider, provider_reference, status, paid_at
FROM payments
WHERE status = 'REFUND_PENDING'
ORDER BY updated_at, id
LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("query payment exceptions: %w", err)
	}
	defer rows.Close()
	exceptions := make([]tickets.PaymentException, 0)
	for rows.Next() {
		var exception tickets.PaymentException
		if err := rows.Scan(&exception.OrderID, &exception.Provider, &exception.Reference, &exception.Status, &exception.PaidAt); err != nil {
			return nil, fmt.Errorf("scan payment exception: %w", err)
		}
		exceptions = append(exceptions, exception)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate payment exceptions: %w", err)
	}
	return exceptions, nil
}

func (r *TicketsRepository) ListNotificationFailures(ctx context.Context, now time.Time, limit int) ([]tickets.NotificationFailure, error) {
	rows, err := r.pool.Query(ctx, `
SELECT id, payload->>'order_id', attempts, status, available_at
FROM outbox_events
WHERE event_type = 'TICKET_DELIVERY_REQUESTED'
  AND ((status = 'PENDING' AND attempts > 0) OR (status = 'PROCESSING' AND available_at <= $1))
ORDER BY available_at, id
LIMIT $2`, now, limit)
	if err != nil {
		return nil, fmt.Errorf("query notification failures: %w", err)
	}
	defer rows.Close()
	failures := make([]tickets.NotificationFailure, 0)
	for rows.Next() {
		var failure tickets.NotificationFailure
		if err := rows.Scan(&failure.EventID, &failure.OrderID, &failure.Attempts, &failure.Status, &failure.AvailableAt); err != nil {
			return nil, fmt.Errorf("scan notification failure: %w", err)
		}
		failures = append(failures, failure)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate notification failures: %w", err)
	}
	return failures, nil
}

type ticketRows interface {
	Next() bool
	Scan(...any) error
	Err() error
}

type adminTicketQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func findAdminTicket(ctx context.Context, queryer adminTicketQueryer, qrToken string, lock bool) (tickets.AdminTicket, error) {
	query := `
SELECT t.id::text, t.status, u.display_name, m.title, c.name, st.name, sh.starts_at, t.checked_in_at
FROM tickets t
JOIN orders o ON o.id = t.order_id
JOIN users u ON u.id = o.user_id
JOIN showtimes sh ON sh.id = o.showtime_id
JOIN movies m ON m.id = sh.movie_id
JOIN studios st ON st.id = sh.studio_id
JOIN cinemas c ON c.id = st.cinema_id
WHERE t.qr_token_hash = ENCODE(DIGEST($1, 'sha256'), 'hex')`
	if lock {
		query += " FOR UPDATE OF t"
	}
	var ticket tickets.AdminTicket
	err := queryer.QueryRow(ctx, query, qrToken).Scan(
		&ticket.ID,
		&ticket.Status,
		&ticket.CustomerDisplayName,
		&ticket.MovieTitle,
		&ticket.CinemaName,
		&ticket.StudioName,
		&ticket.StartsAt,
		&ticket.CheckedInAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return tickets.AdminTicket{}, tickets.ErrTicketNotFound
	}
	if err != nil {
		return tickets.AdminTicket{}, fmt.Errorf("find ticket by QR token: %w", err)
	}
	return ticket, nil
}

func scanTickets(rows ticketRows) ([]tickets.Ticket, error) {
	result := make([]tickets.Ticket, 0)
	for rows.Next() {
		var ticket tickets.Ticket
		if err := rows.Scan(&ticket.ID, &ticket.OrderID, &ticket.UserID, &ticket.Code, &ticket.Status); err != nil {
			return nil, fmt.Errorf("scan ticket: %w", err)
		}
		ticket.QRToken = ticket.Code
		result = append(result, ticket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tickets: %w", err)
	}
	return result, nil
}

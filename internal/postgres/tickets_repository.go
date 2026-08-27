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

type ticketRows interface {
	Next() bool
	Scan(...any) error
	Err() error
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

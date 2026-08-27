package tickets

import (
	"context"
	"errors"
	"sync"
	"time"
)

type memoryDeliveryEvent struct {
	DeliveryEvent
	Status      DeliveryStatus
	AvailableAt time.Time
}

// MemoryRepository supports deterministic service tests.
type MemoryRepository struct {
	mu                   sync.Mutex
	tickets              []Ticket
	events               map[int64]memoryDeliveryEvent
	nextEvent            int64
	expiringHolds        []ExpiringHold
	paymentExceptions    []PaymentException
	notificationFailures []NotificationFailure
}

func NewMemoryRepository(tickets []Ticket) *MemoryRepository {
	return &MemoryRepository{tickets: append([]Ticket(nil), tickets...), events: make(map[int64]memoryDeliveryEvent)}
}

func (r *MemoryRepository) ListOrderTickets(ctx context.Context, orderID, userID string) ([]Ticket, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]Ticket, 0)
	for _, ticket := range r.tickets {
		if ticket.OrderID == orderID && ticket.UserID == userID {
			ticket.TokenHash = ""
			result = append(result, ticket)
		}
	}
	if len(result) == 0 {
		return nil, ErrOrderNotFound
	}
	return result, nil
}

func (r *MemoryRepository) ClaimTicketDeliveries(_ context.Context, now time.Time, limit int, lease time.Duration) ([]DeliveryEvent, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	claimed := make([]DeliveryEvent, 0, limit)
	for id, event := range r.events {
		if len(claimed) == limit || (event.Status != DeliveryPending && event.Status != DeliveryProcessing) || event.AvailableAt.After(now) {
			continue
		}
		event.Status = DeliveryProcessing
		event.Attempts++
		event.AvailableAt = now.Add(lease)
		r.events[id] = event
		claimed = append(claimed, event.DeliveryEvent)
	}
	return claimed, nil
}

func (r *MemoryRepository) LoadDelivery(_ context.Context, event DeliveryEvent) (Delivery, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := Delivery{OrderID: event.OrderID, Email: "customer@example.test"}
	for _, ticket := range r.tickets {
		if ticket.OrderID == event.OrderID {
			ticket.TokenHash = ""
			result.Tickets = append(result.Tickets, ticket)
		}
	}
	if len(result.Tickets) == 0 {
		return Delivery{}, ErrOrderNotFound
	}
	return result, nil
}

func (r *MemoryRepository) CompleteTicketDelivery(_ context.Context, id int64, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.events[id]
	if !ok {
		return errors.New("delivery event not found")
	}
	event.Status = DeliveryCompleted
	r.events[id] = event
	return nil
}

func (r *MemoryRepository) RetryTicketDelivery(_ context.Context, id int64, now time.Time, delay time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	event, ok := r.events[id]
	if !ok {
		return errors.New("delivery event not found")
	}
	event.Status = DeliveryPending
	event.AvailableAt = now.Add(delay)
	r.events[id] = event
	return nil
}

func (r *MemoryRepository) EnqueueDelivery(orderID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextEvent++
	r.events[r.nextEvent] = memoryDeliveryEvent{DeliveryEvent: DeliveryEvent{ID: r.nextEvent, OrderID: orderID}, Status: DeliveryPending}
}

func (r *MemoryRepository) DeliveryState(orderID string) DeliveryStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, event := range r.events {
		if event.OrderID == orderID {
			return event.Status
		}
	}
	return ""
}

func (r *MemoryRepository) LookupAdminTicket(_ context.Context, qrToken string) (AdminTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, ticket := range r.tickets {
		if ticket.QRToken == qrToken {
			return toAdminTicket(ticket), nil
		}
	}
	return AdminTicket{}, ErrTicketNotFound
}

func (r *MemoryRepository) CheckInTicket(_ context.Context, qrToken, _ string, now time.Time) (AdminTicket, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index, ticket := range r.tickets {
		if ticket.QRToken != qrToken {
			continue
		}
		if ticket.Status == TicketUsed {
			return AdminTicket{}, ErrTicketAlreadyUsed
		}
		if ticket.Status != TicketIssued {
			return AdminTicket{}, ErrTicketNotFound
		}
		ticket.Status = TicketUsed
		checkedInAt := now
		ticket.CheckedInAt = &checkedInAt
		r.tickets[index] = ticket
		return toAdminTicket(ticket), nil
	}
	return AdminTicket{}, ErrTicketNotFound
}

func (r *MemoryRepository) ListExpiringHolds(_ context.Context, _ time.Time, limit int) ([]ExpiringHold, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]ExpiringHold(nil), r.expiringHolds[:min(limit, len(r.expiringHolds))]...), nil
}

func (r *MemoryRepository) ListPaymentExceptions(_ context.Context, limit int) ([]PaymentException, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]PaymentException(nil), r.paymentExceptions[:min(limit, len(r.paymentExceptions))]...), nil
}

func (r *MemoryRepository) ListNotificationFailures(_ context.Context, _ time.Time, limit int) ([]NotificationFailure, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]NotificationFailure(nil), r.notificationFailures[:min(limit, len(r.notificationFailures))]...), nil
}

func (r *MemoryRepository) AddExpiringHold(hold ExpiringHold) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expiringHolds = append(r.expiringHolds, hold)
}

func (r *MemoryRepository) AddPaymentException(exception PaymentException) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paymentExceptions = append(r.paymentExceptions, exception)
}

func (r *MemoryRepository) AddNotificationFailure(failure NotificationFailure) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.notificationFailures = append(r.notificationFailures, failure)
}

func toAdminTicket(ticket Ticket) AdminTicket {
	return AdminTicket{ID: ticket.ID, Status: ticket.Status, CustomerDisplayName: "Customer", CheckedInAt: ticket.CheckedInAt}
}

func min(first, second int) int {
	if first < second {
		return first
	}
	return second
}

// MemoryNotifier is a deterministic notifier for tests and local wiring.
type MemoryNotifier struct {
	failuresRemaining int
	deliveries        int
}

func (n *MemoryNotifier) Deliver(ctx context.Context, _ Delivery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n.deliveries++
	if n.failuresRemaining > 0 {
		n.failuresRemaining--
		return errors.New("temporary notification failure")
	}
	return nil
}

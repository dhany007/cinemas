package tickets

import (
	"context"
	"log/slog"
)

// LoggingNotifier is the local development delivery adapter. It deliberately
// omits customer addresses, ticket codes, QR tokens, and hashes from logs.
type LoggingNotifier struct {
	logger *slog.Logger
}

func NewLoggingNotifier(logger *slog.Logger) *LoggingNotifier {
	return &LoggingNotifier{logger: logger}
}

func (n *LoggingNotifier) Deliver(ctx context.Context, delivery Delivery) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n.logger.Info("ticket delivery completed", "order_id", delivery.OrderID, "ticket_count", len(delivery.Tickets))
	return nil
}

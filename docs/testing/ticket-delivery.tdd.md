# TDD Evidence - Ticket Retrieval and Delivery

## Source Plan

- Stage 5 — Tickets and Customer Notifications.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|---|---|---|---|---|
| 1 | A paid-order owner can retrieve opaque QR ticket values; another customer cannot, and no hash is returned. | `internal/tickets/service_test.go:TestServiceListsOnlyPaidOwnersTicketsWithoutHashes` | Unit | Focused package test failed to compile because the ticket service and repository did not exist. | Focused ticket-service tests passed. |
| 2 | A temporary notification error returns the event to pending state and the next attempt completes it. | `internal/tickets/service_test.go:TestServiceRetriesTicketDeliveryAndReconcilesLeaseExpiry` | Unit | Included in the missing ticket-service RED failure. | Focused ticket-service tests passed. |
| 3 | The HTTP ticket endpoint returns data only to its authenticated owner and omits hash fields. | `internal/httpapi/server_test.go:TestServerReturnsTicketsOnlyToTheirOwner` | API | Focused HTTP test failed because `EnableTicketRoutes` did not exist. | Focused HTTP test passed. |
| 4 | Payment finalization creates one random-token ticket and one outbox event; a duplicate webhook cannot create another delivery event. | `internal/postgres/tickets_repository_integration_test.go:TestTicketsRepositoryPostgreSQLRetrievalAndOutbox` | PostgreSQL integration | First disposable PostgreSQL run exposed UUID/text parameter ambiguity in the JSONB outbox payload. | Corrected payload binding; disposable PostgreSQL integration test passed. |

## Validation Commands

- `go test ./...`: passed.
- `go test ./internal/tickets ./internal/httpapi ./internal/postgres -count=1`: passed.
- Disposable Docker PostgreSQL migration and `CINEMAS_TEST_DATABASE_URL=… go test ./internal/postgres -run TestTicketsRepositoryPostgreSQLRetrievalAndOutbox -count=1`: passed.

## Notes

- The ticket code is a `TKT-` prefix plus 32 cryptographically random bytes encoded as hex. Its SHA-256 hash is persisted in `qr_token_hash`; repositories, responses, delivery logs, and outbox payloads never expose that hash.
- The local logging notifier is an operational adapter, not an opt-in customer channel. Customer delivery preferences remain unnecessary until email/SMS/push delivery is introduced.

# TDD Evidence - Seat Hold API Foundation

## Source Plan

- `docs/architecture.md`
- Core implementation slice: transactional seat holds and `POST /v1/orders`.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
| --- | --- | --- | --- | --- | --- |
| 1 | A valid request holds every requested available seat, produces a pending-payment order, and gives all seats the same expiry. | `internal/booking/service_test.go:TestServiceCreateHold` | Unit | `go test ./internal/booking -run 'TestServiceCreateHold'` failed because the booking API did not exist. | The same command passed after the service and repository contract were implemented. |
| 2 | If any selected seat is unavailable, no other selected seat is partially held. | `internal/booking/service_test.go:TestServiceCreateHoldRejectsUnavailableSeatWithoutPartialHold` | Unit | Included in the initial missing-booking API RED failure. | Passed with the focused booking test command. |
| 3 | Repeating an equal request with the same idempotency key returns the existing order. | `internal/booking/service_test.go:TestServiceCreateHoldReturnsExistingOrderForSameIdempotencyKey` | Unit | Included in the initial missing-booking API RED failure. | Passed with the focused booking test command. |
| 4 | The HTTP endpoint creates an order hold and returns a stable order state. | `internal/httpapi/server_test.go:TestServerCreateOrderHold` | API | `go test ./internal/httpapi -run 'TestServerCreateOrderHold'` failed because `NewServer` did not exist. | The same command passed after the Echo server and handler were implemented. |
| 5 | Invalid HTTP requests return a machine-readable validation error. | `internal/httpapi/server_test.go:TestServerCreateOrderHoldReturnsStableValidationError` | API | Included in the missing-server RED failure. | Passed with the focused HTTP test command. |

## Validation Commands

- `go test ./internal/booking -run 'TestServiceCreateHold'`: passed after the booking implementation.
- `go test ./internal/httpapi -run 'TestServerCreateOrderHold'`: passed after the Echo HTTP implementation.
- `go test ./...`: passed.
- `go vet ./...`: passed.
- `go build ./cmd/api`: passed.
- `golangci-lint run ./...`: passed with 0 issues.
- Initial up/down migration: passed against an isolated PostgreSQL 16 container.
- `git diff --check`: passed.

## Coverage and Gaps

- The domain and HTTP contracts are unit/API-tested with a deterministic in-memory repository.
- The PostgreSQL migration up/down path was validated against an ephemeral PostgreSQL 16 container. The repository itself still needs transaction/concurrency integration tests before production.
- Payment-provider webhook finalization, ticket issuance, identity/authentication, staff check-in, outbox processing, and expiry-worker execution are not implemented in this initial backend slice.

## Notes

- `POST /v1/orders` temporarily accepts `user_id` in its request body. It must be replaced by authenticated request identity before production.
- The migration includes the schema required for later payment, ticket, outbox, and audit work; only the seat-hold path is implemented end-to-end in this slice.

# TDD Evidence - Fake Payment Provider

## Source Plan

- `docs/architecture.md`
- Development payment slice: `POST /v1/orders/{orderId}/payment-intents`.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
| --- | --- | --- | --- | --- | --- |
| 1 | A fake payment succeeds for an eligible pending order and issues one ticket per item. | `internal/payments/service_test.go:TestServiceCreateFakePaymentSucceedsAndIssuesTickets` | Unit | `go test ./internal/payments ./internal/httpapi -run 'Test(ServiceCreateFakePayment|ServerCreateFakePayment)'` failed because the payment API did not exist. | The same command passed after the fake payment service and repositories were implemented. |
| 2 | Retrying the same fake payment returns its original successful result without issuing duplicate tickets. | `internal/payments/service_test.go:TestServiceCreateFakePaymentSucceedsAndIssuesTickets` | Unit | Included in the initial missing-payment API RED failure. | Passed with the focused fake-payment test command. |
| 3 | A missing order is represented by a typed not-found error. | `internal/payments/service_test.go:TestServiceCreateFakePaymentRejectsMissingOrder` | Unit | Included in the initial missing-payment API RED failure. | Passed with the focused fake-payment test command. |
| 4 | The API returns a successful fake-provider payment response. | `internal/httpapi/server_test.go:TestServerCreateFakePayment` | API | Included in the initial missing-payment API RED failure. | Passed with the focused fake-payment test command. |

## PostgreSQL Transaction Semantics

- The transaction locks the order, rejects expired or non-pending orders, writes the `FAKE` payment, marks held seats `SOLD`, marks the order `PAID`, and creates tickets.
- Repeated calls for a paid order return the existing fake payment; unique constraints and ticket conflict handling prevent duplicate issuance.
- Existing `payments`, `orders`, `showtime_seats`, `order_items`, and `tickets` tables provide the required schema, so no migration is required.

## Validation Commands

- `go test ./internal/payments ./internal/httpapi -run 'Test(ServiceCreateFakePayment|ServerCreateFakePayment)'`: passed.
- `go test ./...`: passed.
- `go vet ./...`: passed.
- `go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `golangci-lint run -c .golangci.yml ./...`: passed with 0 issues, including `mnd` and `lll`.
- `git diff --check`: passed.

## Coverage and Gaps

- Unit and HTTP tests cover success, retry safety, and missing-order behavior.
- The PostgreSQL transaction still needs integration coverage for locking, expired orders, seat sale, and ticket issuance.
- This provider is development-only. A production adapter must verify signed asynchronous webhook events and record provider-event deduplication.

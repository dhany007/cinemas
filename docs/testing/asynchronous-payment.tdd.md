# TDD Evidence - Asynchronous Payment Flow

## Source Plan

- Stage 4 payment integration, scoped by the decision to use a swappable local mock adapter until a gateway is selected.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|---|---|---|---|---|
| 1 | Creating an intent leaves the order pending; a verified event pays it exactly once. | `internal/payments/service_test.go:TestServiceCreatesPendingIntentAndWebhookFinalizesOnlyOnce` | Unit | Focused test failed to compile because the provider, pending intent, and webhook API did not exist. | Focused payment tests pass. |
| 2 | Invalid signatures and stale timestamps cannot mutate payment state. | `internal/payments/service_test.go:TestServiceRejectsInvalidAndStaleWebhooks` | Unit | Included in the missing webhook API RED failure. | Focused payment tests pass. |
| 3 | A late successful payment is held for refund/manual review without tickets or reclaimed seats. | `internal/payments/service_test.go:TestServiceMarksLatePaymentForRefundWithoutIssuingTickets` | Unit | Included in the missing webhook API RED failure. | Focused payment tests pass. |
| 4 | Failed, expired, and out-of-order provider events never regress a successful payment. | `internal/payments/service_test.go:TestServiceHandlesFailedExpiredAndOutOfOrderEvents` | Unit | Added after the event processor implementation to cover transition rules. | `go test ./internal/payments -count=1` passed. |
| 5 | The public API returns a pending intent and accepts only a signed webhook for finalization. | `internal/httpapi/server_test.go:TestServerCreatesPendingIntentAndAcceptsVerifiedWebhook` | API | The focused test failed because the old synchronous fake-payment method no longer existed. | Focused HTTP test passed. |
| 6 | PostgreSQL atomically finalizes seats, tickets, audit events, and records duplicate/late events safely. | `internal/postgres/payments_repository_integration_test.go:TestPaymentsRepositoryPostgreSQLWebhookFinalization` | PostgreSQL integration | New integration coverage added for the transaction contract. | Disposable PostgreSQL test passed. |

## Validation Commands

- `go test ./...`: passed.
- `go test ./internal/payments -count=1`: passed.
- `go vet ./cmd/api ./internal/httpapi ./internal/payments ./internal/postgres`: passed.
- Disposable Docker PostgreSQL migration and `CINEMAS_TEST_DATABASE_URL=… go test ./internal/postgres -run TestPaymentsRepositoryPostgreSQLWebhookFinalization -count=1`: passed.

## Notes

- `FAKE` is accepted only outside `APP_ENV=production` and requires a webhook secret of at least 32 bytes.
- A real gateway is an adapter replacement: its implementation must preserve the stable idempotency key, verified-event contract, and late-payment refund rule.

# TDD Evidence - Admin Ticket Operations

## Source Plan

- Stage 6 — Admin Ticket Validation and Operations.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|---|---|---|---|---|
| 1 | One QR token changes from `ISSUED` to `USED`; a repeat scan is a conflict and keeps the first timestamp. | `internal/tickets/service_test.go:TestServiceChecksInTicketOnceAndPreservesFirstCheckIn` | Unit | Focused ticket test failed to compile because check-in types and methods did not exist. | Focused ticket-service tests passed. |
| 2 | Operational reads expose expiring holds, refund-pending payments, and retrying delivery events. | `internal/tickets/service_test.go:TestServiceListsOperationalExceptions` | Unit | Included in the missing operations API RED failure. | Focused ticket-service tests passed. |
| 3 | Customer tokens are forbidden from admin lookup; admins see minimal data and receive a repeat-scan conflict. | `internal/httpapi/server_test.go:TestServerAllowsOnlyAdminTicketLookupAndSingleCheckIn` | API | Focused HTTP test returned `404` because admin ticket routes were not registered. | Focused HTTP test passed after admin routes and error mapping were added. |
| 4 | Two concurrent database check-ins produce exactly one success, one conflict, and one audit event. | `internal/postgres/tickets_operations_integration_test.go:TestTicketsRepositoryPostgreSQLCheckInIsAtomic` | PostgreSQL integration | Integration test added for the row-locking contract. | Disposable PostgreSQL integration test passed. |

## Validation Commands

- `go test ./...`: passed.
- `go test ./internal/httpapi -run TestServerAllowsOnlyAdminTicketLookupAndSingleCheckIn -count=1`: passed.
- Disposable Docker PostgreSQL migration and `CINEMAS_TEST_DATABASE_URL=… go test ./internal/postgres -run TestTicketsRepositoryPostgreSQLCheckInIsAtomic -count=1`: passed.

## Notes

- Admin ticket lookup returns only the customer display name and ticket/showtime data; it does not return email, QR token, or token hash.
- Check-in audit records identify the authenticated administrator and ticket. The only mutable Stage 6 operation is check-in; exception views are read-only.

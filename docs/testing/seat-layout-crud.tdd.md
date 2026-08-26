# TDD Evidence - Admin Seat Layout CRUD

## Source Plan

- `docs/roadmap.md`, Stage 2: authenticated admin-only CRUD for seat layouts.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|-----------|-------------|-----------|--------------|----------------|
| 1 | An administrator can create, list, update, and delete physical seats for a studio. | `internal/httpapi/server_test.go:TestServerAdminSeatLayoutCRUD` | HTTP integration | The new route returned `404 Not Found`. | The focused test passes for all four operations. |
| 2 | Only an administrator can manage seat layouts. | `internal/httpapi/server_test.go:TestServerRejectsCustomerSeatLayoutManagement` | HTTP authorization | The new route returned `404 Not Found`. | A customer token receives `403 FORBIDDEN`. |
| 3 | A duplicate `(studio_id, row_label, seat_number)` returns a stable conflict. | `internal/httpapi/server_test.go:TestServerRejectsDuplicateSeatLayoutPosition` | HTTP conflict | The endpoint did not exist. | A duplicate create receives `409 SEAT_ALREADY_EXISTS`. |
| 4 | Seat create, update, and delete actions are audit-recorded. | `internal/admin/service_test.go:TestServiceManagesSeatLayoutAndRecordsAuditEvents` | Domain unit | The test did not compile because the seat service API was absent. | The test verifies the three `SEAT` mutation audits. |

## Validation Commands

- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./internal/admin ./internal/httpapi -run 'Test(ServiceManagesSeatLayoutAndRecordsAuditEvents|ServerAdminSeatLayoutCRUD|ServerRejectsCustomerSeatLayoutManagement|ServerRejectsDuplicateSeatLayoutPosition)' -count=1`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go vet ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/cinemas-golangci-cache golangci-lint run -c .golangci.yml ./internal/admin ./internal/httpapi ./internal/postgres`: passed with `0 issues`.
- `ruby -e 'require "yaml"; YAML.load_file("openapi/openapi.yaml")'`: passed.
- `git diff --check`: passed.

## Notes

- The `seats` table already enforces uniqueness for each physical position in a studio; the memory repository and PostgreSQL repository both map that rule to `SEAT_ALREADY_EXISTS`.
- Every successful mutation writes its matching audit event in the same repository transaction.
- Preventing layout changes after showtimes exist remains the separate roadmap item and is intentionally not included here.

# TDD Evidence - Admin Cinema CRUD

## Source Plan

- `docs/roadmap.md`, Stage 2: authenticated admin-only CRUD for cinemas.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|-----------|-------------|-----------|--------------|----------------|
| 1 | An administrator can create, list, get, update, and delete a cinema through the API. | `internal/httpapi/server_test.go:TestServerAdminCinemaCRUD` | HTTP integration | The focused test returned `405 Method Not Allowed` for `GET /v1/admin/cinemas` because only the create route existed. | The focused test passed after the CRUD routes and handlers were implemented. |
| 2 | A customer cannot access cinema administration routes. | `internal/httpapi/server_test.go:TestServerRejectsCustomerCinemaManagement` | HTTP authorization | The focused test returned `405 Method Not Allowed` because the list route did not exist. | The focused test passed and returned `403 FORBIDDEN`. |
| 3 | Every cinema mutation creates an audit event. | `internal/httpapi/server_test.go:TestServerAdminCinemaCRUD` | HTTP integration | Covered by the missing route RED state above. | The test verifies exactly three events for create, update, and delete. |

## Validation Commands

- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./internal/httpapi -run 'TestServerAdminCinemaCRUD|TestServerRejectsCustomerCinemaManagement' -count=1`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/cinemas-golangci-cache golangci-lint run -c .golangci.yml ./...`: passed with `0 issues`.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go vet ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go build -o /private/tmp/cinemas-api ./cmd/api`: passed.

## Coverage / Gaps

- The contract tests use the in-memory repository and cover API behavior, authorization, validation through the service, and audit-event counts.
- PostgreSQL transaction behavior is validated by the repository implementation review and will be exercised by the existing migration smoke test during full validation.
- `golangci-lint config verify` could not complete because its remote JSON schema endpoint timed out; the configured full lint run itself completed successfully.

## Notes

- Cinema updates replace `name`, `address`, and `city`; partial updates are intentionally not supported by this request contract.
- PostgreSQL performs each create, update, and delete together with its audit insert in one transaction.

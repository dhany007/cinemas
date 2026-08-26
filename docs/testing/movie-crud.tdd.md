# TDD Evidence - Admin Movie CRUD

## Source Plan

- `docs/roadmap.md`, Stage 2: authenticated admin-only CRUD for movies.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|-----------|-------------|-----------|--------------|----------------|
| 1 | An administrator can create, list, update, and delete movie metadata through the API. | `internal/httpapi/server_test.go:TestServerAdminMovieCRUD` | HTTP integration | The create route returned `404 Not Found`. | The focused test passes for all four operations. |
| 2 | A customer cannot access movie administration routes. | `internal/httpapi/server_test.go:TestServerRejectsCustomerMovieManagement` | HTTP authorization | The list route returned `404 Not Found`. | A customer token receives `403 FORBIDDEN`. |
| 3 | Invalid required metadata receives a stable validation response. | `internal/httpapi/server_test.go:TestServerRejectsInvalidMovieMetadata` | HTTP validation | The endpoint did not exist. | A zero duration receives `400 INVALID_REQUEST`. |
| 4 | Movie creation, update, and deletion write matching audit events. | `internal/admin/service_test.go:TestServiceManagesMovieAndRecordsAuditEvents` | Domain unit | The test did not compile because the movie service API was absent. | The test verifies all three `MOVIE` mutations. |

## Validation Commands

- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./internal/admin ./internal/httpapi -run 'Test(ServiceManagesMovieAndRecordsAuditEvents|ServerAdminMovieCRUD|ServerRejectsCustomerMovieManagement|ServerRejectsInvalidMovieMetadata)' -count=1`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go vet ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/cinemas-golangci-cache golangci-lint run -c .golangci.yml ./internal/admin ./internal/httpapi ./internal/postgres`: passed with `0 issues`.
- `ruby -e 'require "yaml"; YAML.load_file("openapi/openapi.yaml")'`: passed.
- `git diff --check`: passed.

## Notes

- `title` and a positive `duration_minutes` are required. Optional text values are trimmed and blank values are persisted as `NULL`; an optional release date must use `YYYY-MM-DD`, and a poster URL must be an absolute URI.
- Every successful mutation writes its matching audit event in the same repository transaction.
- PostgreSQL foreign keys continue to prevent deleting a movie that has dependent showtimes. Policies for editing data with active or historical showtimes remain separate roadmap work.

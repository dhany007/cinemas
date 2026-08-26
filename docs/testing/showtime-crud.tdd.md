# TDD Evidence - Admin Showtime CRUD and Seat Materialization

## Source Plan

- `docs/roadmap.md`, Stage 2: authenticated admin-only CRUD for showtimes and materialized `showtime_seats`.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|-----------|-------------|-----------|--------------|----------------|
| 1 | An administrator can create, list, update, and delete a showtime through the API. | `internal/httpapi/server_test.go:TestServerAdminShowtimeCRUD` | HTTP integration | The create route returned `404 Not Found`. | The focused test passes for all four operations. |
| 2 | Creating and updating a showtime materializes physical studio seats using the showtime price and currency snapshot. | `internal/admin/service_test.go:TestServiceManagesShowtimeAndMaterializesSeats` | Domain unit | The test did not compile because the showtime service API and inventory snapshot were absent. | The test verifies the initial and replacement seat-price snapshots. |
| 3 | A customer cannot manage showtimes. | `internal/httpapi/server_test.go:TestServerRejectsCustomerShowtimeManagement` | HTTP authorization | The list route returned `404 Not Found`. | A customer token receives `403 FORBIDDEN`. |
| 4 | An invalid time range receives a stable validation response. | `internal/httpapi/server_test.go:TestServerRejectsInvalidShowtimeMetadata` | HTTP validation | The endpoint did not exist. | A start time at or after the end time receives `400 INVALID_REQUEST`. |

## Validation Commands

- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./internal/admin ./internal/httpapi -run 'Test(ServiceManagesShowtimeAndMaterializesSeats|ServerAdminShowtimeCRUD|ServerRejectsCustomerShowtimeManagement|ServerRejectsInvalidShowtimeMetadata)' -count=1`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go vet ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/cinemas-golangci-cache golangci-lint run -c .golangci.yml ./internal/admin ./internal/httpapi ./internal/postgres`: passed with `0 issues`.
- `ruby -e 'require "yaml"; YAML.load_file("openapi/openapi.yaml")'`: passed.
- `git diff --check`: passed.

## Notes

- The create transaction inserts the showtime, copies every seat in its studio into `showtime_seats` with `AVAILABLE` status, writes the admin audit event, and commits as one unit.
- Updates recreate the materialized inventory using the replacement studio, price, and currency. PostgreSQL rejects an update or deletion as `SHOWTIME_IN_USE` when dependent order inventory prevents replacement.
- Overlap detection remains the separate next roadmap item and is intentionally not included in this feature.

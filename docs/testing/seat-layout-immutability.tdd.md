# TDD Evidence - Seat Layout Immutability After Showtime Creation

## Source Plan

- `docs/roadmap.md`, Stage 2: prevent seat-layout changes that would invalidate active or historical showtimes.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|-----------|-------------|-----------|--------------|----------------|
| 1 | Once a studio has a showtime, adding, materially updating, or deleting a physical seat is rejected. | `internal/admin/service_test.go:TestServiceManagesShowtimeAndMaterializesSeats` | Domain unit | The test did not compile because `ErrSeatLayoutInUse` was absent. | All three operations return `ErrSeatLayoutInUse`. |
| 2 | An administrator receives a stable conflict response when attempting the blocked API operations. | `internal/httpapi/server_test.go:TestServerAdminShowtimeCRUD` | HTTP integration | A post-showtime seat create returned `201 Created`. | Create, update, and delete return `409`; create includes `SEAT_LAYOUT_IN_USE`. |

## Validation Commands

- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./internal/admin ./internal/httpapi -run 'Test(ServiceManagesShowtimeAndMaterializesSeats|ServerAdminShowtimeCRUD)' -count=1`: passed.
- `./scripts/test-migrations.sh`: passed against a temporary PostgreSQL Docker Compose stack; its temporary containers, network, and volume were removed by the script.
- Direct PostgreSQL trigger verification: passed; inserting a seat after fixture showtime creation raised `seat layout cannot change after a showtime exists`.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go vet ./...`: passed (the sandbox emitted a non-fatal module stat-cache write warning after completion).
- `GOCACHE=/private/tmp/cinemas-go-build-cache go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/cinemas-golangci-cache golangci-lint run -c .golangci.yml ./internal/admin ./internal/httpapi ./internal/postgres`: passed with `0 issues`.
- `ruby -e 'require "yaml"; YAML.load_file("openapi/openapi.yaml")'`: passed.
- `git diff --check`: passed.

## Notes

- PostgreSQL enforces the invariant with `BEFORE` triggers on `seats`, so direct database writers cannot bypass the API rule.
- The trigger also takes a transaction-scoped advisory lock keyed by studio. A matching `showtimes` trigger takes the same lock before insert or update, preventing a concurrent layout change from racing with showtime creation.
- The rule treats every stored showtime as active or historical. An update that leaves all layout fields unchanged is allowed because it cannot invalidate a showtime snapshot.

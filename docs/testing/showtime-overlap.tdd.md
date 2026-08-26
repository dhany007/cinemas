# TDD Evidence - Reject Overlapping Showtimes

## Source Plan

- `docs/roadmap.md`, Stage 2: reject overlapping showtimes in one studio.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|-----------|-------------|-----------|--------------|----------------|
| 1 | Creating a showtime that overlaps an existing screening in the same studio is rejected with a stable conflict response. | `internal/httpapi/server_test.go:TestServerAdminShowtimeCRUD` | HTTP integration | The second, overlapping create returned `201 Created`. | The same request returns `409` with `SHOWTIME_OVERLAP`. |
| 2 | Showtimes may be adjacent, but an update cannot move a showtime into another show's interval. | `internal/admin/service_test.go:TestServiceManagesShowtimeAndMaterializesSeats` | Domain unit | The overlap sentinel did not exist and the test did not compile. | An adjacent create succeeds and an overlapping update returns `ErrShowtimeOverlap`. |

## Validation Commands

- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./internal/admin ./internal/httpapi -run 'Test(ServiceManagesShowtimeAndMaterializesSeats|ServerAdminShowtimeCRUD)' -count=1`: passed.
- `./scripts/test-migrations.sh`: passed against a temporary PostgreSQL Docker Compose stack; its temporary containers, network, and volume were removed by the script.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go vet ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/cinemas-golangci-cache golangci-lint run -c .golangci.yml ./internal/admin ./internal/httpapi ./internal/postgres`: passed with `0 issues`.
- `ruby -e 'require "yaml"; YAML.load_file("openapi/openapi.yaml")'`: passed.
- `git diff --check`: passed.

## Notes

- PostgreSQL enforces the rule with a GiST exclusion constraint over `(studio_id, tstzrange(starts_at, ends_at, '[)'))`. The half-open time range allows an ending showtime and the next showtime to share the boundary instant without overlapping.
- The `btree_gist` extension supplies the GiST equality operator for UUID studio IDs. The down migration intentionally retains the extension because it can be shared by other database objects.
- The database constraint, rather than an application-level pre-check, prevents concurrent create or update requests from producing an invalid schedule. Existing overlapping rows would cause the forward migration to fail safely and must be corrected before it is applied.

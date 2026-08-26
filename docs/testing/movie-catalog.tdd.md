# TDD Evidence - Public Movie Catalog API

## Source Plan

- `docs/architecture.md`
- Public catalog read slice: `GET /v1/movies`.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
| --- | --- | --- | --- | --- | --- |
| 1 | A public movie request returns a deterministic first page and an opaque cursor when further records exist. | `internal/catalog/service_test.go:TestServiceListMoviesReturnsNextCursor` | Unit | `go test ./internal/catalog ./internal/httpapi -run 'Test(ServiceListMovies|ParsePageSize|ServerListMovies)'` failed because the catalog API did not exist. | The same focused command passed after the catalog service and repositories were implemented. |
| 2 | Reusing a returned cursor advances to the next movie rather than repeating the first item. | `internal/catalog/service_test.go:TestServiceListMoviesReturnsNextCursor` | Unit | Included in the missing-catalog API RED failure. | Passed with the focused catalog test command. |
| 3 | Page-size validation uses a default and rejects limits outside the supported range. | `internal/catalog/service_test.go:TestParsePageSize` | Unit | Included in the missing-catalog API RED failure. | Passed with the focused catalog test command. |
| 4 | The HTTP endpoint returns public movie metadata. | `internal/httpapi/server_test.go:TestServerListMovies` | API | Included in the missing-catalog API RED failure. | Passed with the focused catalog test command. |
| 5 | Invalid limits and opaque cursors map to a stable client validation error. | `internal/httpapi/server_test.go:TestServerListMoviesRejectsInvalidLimit`, `internal/httpapi/server_test.go:TestServerListMoviesRejectsInvalidCursor` | API | The initial missing-catalog API RED failure included the new route. | Both tests passed after the handler mapped catalog validation errors to `400 INVALID_REQUEST`. |

## PostgreSQL Query and Migration

- The repository uses parameterized keyset pagination ordered by `created_at DESC, id DESC`.
- `migrations/000002_add_movies_created_id_index.up.sql` adds the matching B-tree index; its down migration is for local development and tests only.
- The initial schema plus the new index migration were applied successfully in an isolated PostgreSQL 16 container; the new down migration then removed the index. No external or production database was changed.

## Validation Commands

- `go test ./internal/catalog ./internal/httpapi -run 'Test(ServiceListMovies|ParsePageSize|ServerListMovies)'`: passed.
- `go test ./...`: passed.
- `go vet ./...`: passed.
- `go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `golangci-lint config verify -c .golangci.yml`: passed; `lll` is enabled.
- `golangci-lint run -c .golangci.yml ./...`: passed with 0 issues.
- PostgreSQL 16 migration smoke test: initial schema, `000002` up, and `000002` down all passed in an isolated container.
- `git diff --check`: passed.

## Coverage and Gaps

- Unit and HTTP tests cover page construction, cursor progression, and client-visible validation errors.
- The PostgreSQL repository and migration still need ephemeral-PostgreSQL integration coverage for actual query execution and the index migration path.
- No authentication, filtering, search, or showtime projection is included in this public catalog slice.

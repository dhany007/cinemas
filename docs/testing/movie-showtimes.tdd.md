# TDD Evidence - Public Movie Showtimes API

## Source Plan

- `docs/architecture.md`
- Public catalog read slice: `GET /v1/movies/{movieId}/showtimes?date=YYYY-MM-DD`.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
| --- | --- | --- | --- | --- | --- |
| 1 | A movie's screening includes venue, time, and price information for the selected date. | `internal/scheduling/service_test.go:TestServiceListMovieShowtimes` | Unit | `go test ./internal/scheduling ./internal/httpapi -run 'Test(ServiceListMovieShowtimes|ParseDate|ServerListMovieShowtimes)'` initially failed because the scheduling API did not exist. | The same command passed after the service and repositories were implemented. |
| 2 | A requested movie that does not exist returns a typed not-found error. | `internal/scheduling/service_test.go:TestServiceListMovieShowtimesReturnsNotFound` | Unit | Included in the missing-scheduling API RED failure. | Passed with the focused showtime test command. |
| 3 | A date is required and must use the `YYYY-MM-DD` format. | `internal/scheduling/service_test.go:TestParseDate` | Unit | Included in the missing-scheduling API RED failure. | Passed with the focused showtime test command. |
| 4 | The HTTP endpoint returns public showtime data for a valid movie and date. | `internal/httpapi/server_test.go:TestServerListMovieShowtimes` | API | Included in the missing-scheduling API RED failure. | Passed with the focused showtime test command. |
| 5 | Invalid dates map to `400 INVALID_REQUEST`, while unknown movies map to `404 MOVIE_NOT_FOUND`. | `internal/httpapi/server_test.go:TestServerListMovieShowtimesRejectsInvalidDate`, `internal/httpapi/server_test.go:TestServerListMovieShowtimesReturnsNotFound` | API | The initial missing-scheduling API RED failure included the new route. | Both tests passed after stable handler error mapping was added. |

## PostgreSQL Query Semantics

- The query reads `showtimes` joined with `studios` and `cinemas`, returning only public venue and pricing metadata.
- It filters a UTC day with `starts_at >= date` and `starts_at < date + 1 day`, then orders by `starts_at, id`.
- The existing `showtimes_by_movie_start (movie_id, starts_at)` B-tree index supports the movie and date-range predicate, so no schema migration is required.

## Validation Commands

- `go test ./internal/scheduling ./internal/httpapi -run 'Test(ServiceListMovieShowtimes|ParseDate|ServerListMovieShowtimes)'`: passed.
- `go test ./...`: passed.
- `go vet ./...`: passed.
- `go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `golangci-lint run -c .golangci.yml ./...`: passed with 0 issues, including `mnd` and `lll`.
- `git diff --check`: passed.

## Coverage and Gaps

- Unit and HTTP tests cover showtime mapping plus the client-visible validation and not-found paths.
- The PostgreSQL repository still needs an ephemeral-PostgreSQL integration test for joins, UTC boundaries, and database error paths.
- The chosen UTC date rule is explicit for this MVP; cinema-local time zones need a data-model decision before multi-city scheduling is introduced.

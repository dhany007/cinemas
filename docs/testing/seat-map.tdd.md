# TDD Evidence - Public Seat Map API

## Source Plan

- `docs/architecture.md`
- Public inventory read slice: `GET /v1/showtimes/{showtimeId}/seats`.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
| --- | --- | --- | --- | --- | --- |
| 1 | The inventory service returns the price and availability for every seat in a known showtime. | `internal/seatinventory/service_test.go:TestServiceListSeatMap` | Unit | `go test ./internal/seatinventory ./internal/httpapi -run 'Test(ServiceListSeatMap|ServerGetShowtimeSeats)'` initially failed because the seat-inventory API did not exist. | The same command passed after the service, repository contract, and HTTP handler were implemented. |
| 2 | A missing showtime is represented by a typed not-found error. | `internal/seatinventory/service_test.go:TestServiceListSeatMapReturnsNotFound` | Unit | Included in the missing seat-inventory API RED failure. | Passed with the focused seat-map test command. |
| 3 | The public endpoint returns the seat map without exposing order or hold ownership. | `internal/httpapi/server_test.go:TestServerGetShowtimeSeats` | API | Included in the missing seat-inventory API RED failure. | Passed with the focused seat-map test command. |
| 4 | A valid-but-missing UUID showtime receives a stable not-found response. | `internal/httpapi/server_test.go:TestServerGetShowtimeSeatsReturnsNotFound` | API | Included in the missing seat-inventory API RED failure. | Passed with the focused seat-map test command. |
| 5 | A malformed showtime identifier is rejected before it reaches PostgreSQL. | `internal/httpapi/server_test.go:TestServerGetShowtimeSeatsReturnsValidationErrorForInvalidShowtimeID` | API | `go test ./internal/httpapi -run TestServerGetShowtimeSeatsReturnsValidationErrorForInvalidShowtimeID` failed with `404`, where `400` was required. | The focused seat-map test command passed after UUID validation was added. |

## PostgreSQL Read Semantics

- The repository first checks that the showtime exists, distinguishing an empty seat map from an unknown showtime.
- It reads inventory through `showtime_seats` joined to physical `seats`, using the existing `showtime_id` index.
- The query evaluates expiry with PostgreSQL `now()`: a `HELD` seat whose `hold_expires_at` has elapsed is returned as `AVAILABLE`, even before an expiry worker changes the stored status.

## Validation Commands

- `go test ./internal/seatinventory ./internal/httpapi -run 'Test(ServiceListSeatMap|ServerGetShowtimeSeats)'`: passed.
- `go test ./...`: passed.
- `go vet ./...`: passed.
- `go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `golangci-lint config verify -c .golangci.yml`: passed; `lll` is enabled.
- `golangci-lint run -c .golangci.yml ./...`: passed with 0 issues.
- `git diff --check`: passed.

## Coverage and Gaps

- Service and HTTP contracts are covered by deterministic in-memory repository tests.
- The PostgreSQL query is compiled by the application build, but it still needs integration coverage against PostgreSQL for expiry, ordering, and query error paths.
- Authentication and rate limiting for public catalog endpoints remain future work.

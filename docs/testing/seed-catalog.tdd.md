# TDD Evidence — Local Catalog Seed

## Source plan

- `Makefile` `seed` target: local cinemas, studios, and OMDb movie metadata.

## Testable guarantees

| Guarantee | Test target | RED evidence | GREEN evidence |
|---|---|---|---|
| The seed creates cinemas, studios, and OMDb-derived movies only when missing. | `internal/seed/seed_test.go:TestRunnerSeedsCinemaStudiosAndOMDbMoviesIdempotently` | The test initially failed to compile because the runner did not exist. It then exposed a fixture JSON mismatch that caused duplicate studios. | The completed test runs the seed twice and verifies one cinema, two studios, one movie, one OMDb request, a 120-minute duration, and an ISO release date. |
| Missing OMDb credentials fail before API writes. | `internal/seed/seed_test.go:TestConfigValidationRejectsMissingOMDbKey` | The test initially failed because configuration validation did not exist. | The runner returns an error naming `OMDB_API_KEY` before bootstrap/login or catalog calls. |

## Validation commands

```text
go test ./internal/seed
go vet ./internal/seed ./cmd/seed
docker compose config --quiet
make -n seed
```

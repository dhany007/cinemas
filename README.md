# Cinemas Backend

The backend is a Go/Echo API for cinema seat holds. PostgreSQL is the source of truth for seat inventory and order state.

## Prerequisites

- Go 1.25 or a compatible later version
- PostgreSQL 16+
- A migration runner that executes the SQL files in `migrations/` in lexical order (the architecture recommends `goose`)

## Run locally

1. Create a PostgreSQL database and apply `migrations/000001_initial.up.sql` with the project's migration runner.
2. Set `DATABASE_URL` to a least-privilege application connection string.
3. Run `go run ./cmd/api`.

The API listens on `:8080` by default. Set `ADDR` to override it. `GET /healthz` is the liveness endpoint.

## Implemented API

`GET /v1/showtimes/{showtimeId}/seats` returns the public seat map for a UUID showtime. Each seat includes its price and current availability (`AVAILABLE`, `HELD`, `SOLD`, or `BLOCKED`); expired holds are returned as `AVAILABLE`. The response does not expose an order or hold owner.

`POST /v1/orders` creates an atomic, ten-minute seat hold. It requires an `Idempotency-Key` header and a JSON body:

```json
{
  "user_id": "UUID",
  "showtime_id": "UUID",
  "seat_ids": ["UUID", "UUID"]
}
```

The current endpoint accepts `user_id` only as an MVP bootstrap until the identity module is implemented. It must be replaced with authenticated request identity before production.

## Validation

```text
go test ./...
go vet ./...
go build ./cmd/api
```

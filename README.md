# Cinemas Backend

The backend is a Go/Echo API for cinema seat holds. PostgreSQL is the source of truth for seat inventory and order state.

## Prerequisites

- Go 1.25 or a compatible later version
- PostgreSQL 16+
- A migration runner that executes the SQL files in `migrations/` in lexical order (the architecture recommends `goose`)

## Run locally

1. Create a PostgreSQL database and apply every `migrations/*.up.sql` file in lexical order with the project's migration runner.
2. Set `DATABASE_URL` to a least-privilege application connection string.
3. Set `AUTH_JWT_SECRET` to a unique secret of at least 32 bytes. Optionally set `AUTH_ACCESS_TOKEN_TTL` (default `1h`) and `AUTH_ADMIN_BOOTSTRAP_TOKEN` for the one-time admin bootstrap endpoint.
4. Run `go run ./cmd/api`.

The API listens on `:8080` by default. Set `ADDR` to override it. `GET /healthz` is the liveness endpoint.

## Run with Docker Compose

For local development, Docker Compose starts PostgreSQL, applies pending SQL migrations exactly once, then starts the API:

```text
docker compose up --build
```

The API is available at `http://127.0.0.1:18081/healthz` and PostgreSQL is available only on `127.0.0.1:54329` by default. Override those host ports with `CINEMAS_API_PORT` and `CINEMAS_POSTGRES_PORT`.

The Compose database credentials and authentication secrets are local-development values only. Never reuse them in staging or production. To stop the stack while preserving local data, run `docker compose down`. `docker compose down -v` also deletes the local database volume.

## Implemented API

`GET /v1/movies` returns public movie metadata in deterministic newest-first order. It accepts an optional `limit` from `1` to `100` (default `20`) and an opaque `cursor`; pass `next_cursor` from one response as the next request's `cursor` value. A malformed limit or cursor returns `400 INVALID_REQUEST`.

`GET /v1/movies/{movieId}/showtimes?date=YYYY-MM-DD` returns public screenings for a UUID movie on the supplied UTC calendar date. Each screening includes the cinema and studio, start/end times, and price. A missing movie returns `404 MOVIE_NOT_FOUND`; malformed IDs or dates return `400 INVALID_REQUEST`.

`GET /v1/showtimes/{showtimeId}/seats` returns the public seat map for a UUID showtime. Each seat includes its price and current availability (`AVAILABLE`, `HELD`, `SOLD`, or `BLOCKED`); expired holds are returned as `AVAILABLE`. The response does not expose an order or hold owner.

`POST /v1/auth/register` creates a `CUSTOMER` account from `email`, `password`, and `display_name`, then returns a bearer access token. Passwords must be at least 12 characters. `POST /v1/auth/login` accepts `email` and `password` and returns a new bearer access token. The API stores only bcrypt password hashes.

`POST /v1/auth/bootstrap-admin` creates the one initial `ADMIN` account. It accepts the same registration body and requires the `X-Admin-Bootstrap-Token` header to match `AUTH_ADMIN_BOOTSTRAP_TOKEN`. Do not expose this environment secret to browser clients. The endpoint returns `401` when the token is absent or invalid and `409 ADMIN_ALREADY_BOOTSTRAPPED` after the first admin exists.

`POST /v1/orders/{orderId}/payment-intents` requires the customer's bearer token. It uses the local `FAKE` provider during development, synchronously returns `SUCCEEDED`, atomically marks the caller's eligible order paid, changes its held seats to `SOLD`, and issues tickets. It is a development-only provider and must be replaced by a signed asynchronous gateway webhook before production.

`POST /v1/orders` creates an atomic, ten-minute seat hold. It requires `Authorization: Bearer <access_token>`, an `Idempotency-Key` header, and a JSON body:

```json
{
  "showtime_id": "UUID",
  "seat_ids": ["UUID", "UUID"]
}
```

The order owner always comes from the validated access token. A `user_id` in the JSON body is ignored and cannot be used to place an order for another account. Customers may only pay their own orders; unknown and non-owned orders both return `404 ORDER_NOT_FOUND`.

Authentication errors use the standard error envelope: missing or invalid access tokens return `401 UNAUTHENTICATED`, invalid login details return `401 INVALID_CREDENTIALS`, and an authenticated principal without the required role returns `403 FORBIDDEN`.

## Validation

```text
go test ./...
go vet ./...
go build ./cmd/api
```

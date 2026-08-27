# Cinemas

The platform consists of a Go/Echo API, PostgreSQL as the source of truth for seat inventory and order state, and two Next.js applications.

## Prerequisites

- Go 1.25 or a compatible later version
- PostgreSQL 16+
- A migration runner that executes the SQL files in `migrations/` in lexical order (the architecture recommends `goose`)
- Node.js 20+ and pnpm 10+ for the web applications

## Run locally

1. Create a PostgreSQL database and apply every `migrations/*.up.sql` file in lexical order with the project's migration runner.
2. Set `DATABASE_URL` to a least-privilege application connection string.
3. Set `AUTH_JWT_SECRET` to a unique secret of at least 32 bytes. Optionally set `AUTH_ACCESS_TOKEN_TTL` (default `1h`) and `AUTH_ADMIN_BOOTSTRAP_TOKEN` for the one-time admin bootstrap endpoint.
4. Run `go run ./cmd/api`.

The API listens on `:8080` by default. Set `ADDR` to override it. `GET /healthz` is the liveness endpoint.

## Run the web applications

Start the API first (Docker Compose exposes it at `http://127.0.0.1:18081`), then install the JavaScript workspace dependencies once:

```text
pnpm install
```

Run the customer and administrator applications in separate terminals:

```text
API_BASE_URL=http://127.0.0.1:18081 pnpm dev:customer
API_BASE_URL=http://127.0.0.1:18081 pnpm dev:admin
```

They are intentionally independent applications:

- `apps/customer-web` provides the movie catalog, seat selection, hold/payment status, order history, and tickets.
- `apps/admin-web` provides cinema and layout management, movie/showtime CRUD, ticket validation, and operational exception views.

Both apps call their own `/api/backend/*` route handler from the browser. That server-side handler reads `API_BASE_URL` and the HTTP-only `cinemas_access_token` cookie, then adds the bearer token while forwarding to `/v1/*`. Do not set `API_BASE_URL` with a `NEXT_PUBLIC_` prefix and do not put API secrets or bearer tokens in browser code.

## Run with Docker Compose

For local development, `make up` builds and starts PostgreSQL, applies pending SQL migrations exactly once, then starts the API and both web applications in the background:

```text
make up
```

Use `make ps` to inspect service status, `make logs` to follow logs, and `make down` to stop the stack while preserving the local PostgreSQL volume.

## Seed local catalog data

`make seed` creates two local cinemas, three studios, and three movies through the protected administrator API. It is idempotent, so rerunning it creates only missing records. Movie metadata is retrieved from [OMDb](https://www.omdbapi.com/); create a personal development API key, then keep it only in your untracked `.env` file:

```text
cp .env.example .env
# Set OMDB_API_KEY in .env
make seed
```

The seed command bootstraps `admin@cinemas.local` only when no admin exists, then logs in with `SEED_ADMIN_EMAIL` and `SEED_ADMIN_PASSWORD`. If your database already has an admin, configure those variables in `.env` to match that account. It does not print the OMDb key, password, or access token.

The API is available at `http://127.0.0.1:18081/healthz`, the customer web app at `http://127.0.0.1:13000`, and the admin web app at `http://127.0.0.1:13001`. PostgreSQL is available only on `127.0.0.1:54329` by default. Override those host ports with `CINEMAS_API_PORT`, `CINEMAS_CUSTOMER_WEB_PORT`, `CINEMAS_ADMIN_WEB_PORT`, and `CINEMAS_POSTGRES_PORT`.

The Compose database credentials and authentication secrets are local-development values only. Never reuse them in staging or production. To stop the stack while preserving local data, run `docker compose down`. `docker compose down -v` also deletes the local database volume.

Apply pending migrations to the already-running local database with:

```text
docker compose run --rm migrate
```

Run the isolated migration smoke test with:

```text
./scripts/test-migrations.sh
```

The smoke test starts a separate PostgreSQL container without publishing a host port, verifies every migration, and removes only its own temporary volume when it finishes.

## Implemented API

The versioned API contract is [openapi/openapi.yaml](openapi/openapi.yaml). Update it with every client-visible endpoint or response change.

`GET /v1/movies` returns public movie metadata in deterministic newest-first order. It accepts an optional `limit` from `1` to `100` (default `20`) and an opaque `cursor`; pass `next_cursor` from one response as the next request's `cursor` value. A malformed limit or cursor returns `400 INVALID_REQUEST`.

`GET /v1/movies/{movieId}/showtimes?date=YYYY-MM-DD` returns public screenings for a UUID movie on the supplied UTC calendar date. Each screening includes the cinema and studio, start/end times, and price. A missing movie returns `404 MOVIE_NOT_FOUND`; malformed IDs or dates return `400 INVALID_REQUEST`.

`GET /v1/showtimes/{showtimeId}/seats` returns the public seat map for a UUID showtime. Each seat includes its price and current availability (`AVAILABLE`, `HELD`, `SOLD`, or `BLOCKED`); expired holds are returned as `AVAILABLE`. The response does not expose an order or hold owner.

`POST /v1/auth/register` creates a `CUSTOMER` account from `email`, `password`, and `display_name`, then returns a bearer access token. Passwords must be at least 12 characters. `POST /v1/auth/login` accepts `email` and `password` and returns a new bearer access token. The API stores only bcrypt password hashes.

`POST /v1/auth/bootstrap-admin` creates the one initial `ADMIN` account. It accepts the same registration body and requires the `X-Admin-Bootstrap-Token` header to match `AUTH_ADMIN_BOOTSTRAP_TOKEN`. Do not expose this environment secret to browser clients. The endpoint returns `401` when the token is absent or invalid and `409 ADMIN_ALREADY_BOOTSTRAPPED` after the first admin exists.

`POST /v1/orders/{orderId}/payment-intents` requires the customer's bearer token and creates a `PENDING` intent. It never marks an order paid. In local development, the deterministic `FAKE` adapter is enabled with `PAYMENT_PROVIDER=FAKE`; it is rejected when `APP_ENV=production`. A signed `POST /v1/webhooks/payments/FAKE` event is the only path that can mark the order paid, sell seats, issue tickets, and record the payment audit event. The webhook requires `X-Payment-Timestamp` and HMAC-SHA256 `X-Payment-Signature` over `<timestamp>.<raw-body>`, using `PAYMENT_WEBHOOK_SECRET`; events outside `PAYMENT_WEBHOOK_REPLAY_WINDOW` are rejected.

`GET /v1/orders/{orderId}/tickets` requires the owner's bearer token and returns tickets only after the order is paid. Each ticket includes an opaque QR token but never the stored token hash. Payment finalization writes one `TICKET_DELIVERY_REQUESTED` outbox event in the same transaction; the local worker delivers it through a logging adapter and retries failures without logging addresses, ticket codes, QR tokens, or hashes.

Administrators can use `GET /v1/admin/tickets/{qrToken}` for a minimal ticket lookup and `POST /v1/admin/tickets/{qrToken}/check-in` to atomically consume an `ISSUED` ticket. A repeated scan returns `409 TICKET_ALREADY_USED` without changing the original check-in. The admin operational endpoints list holds expiring within fifteen minutes, late payments awaiting refund/manual review, and retrying or stale ticket-delivery events.

`POST /v1/orders` creates an atomic, ten-minute seat hold. It requires `Authorization: Bearer <access_token>`, an `Idempotency-Key` header, and a JSON body:

```json
{
  "showtime_id": "UUID",
  "seat_ids": ["UUID", "UUID"]
}
```

The order owner always comes from the validated access token. A `user_id` in the JSON body is ignored and cannot be used to place an order for another account. Customers may only create an intent for their own order; unknown and non-owned orders both return `404 ORDER_NOT_FOUND`. Provider events are stored idempotently by `(provider, provider_event_id)`. A successful payment received after hold expiry is retained as `REFUND_PENDING`, releases no sold inventory, and produces a `PAYMENT_REFUND_PENDING` audit event for manual review/refund processing.

Authentication errors use the standard error envelope: missing or invalid access tokens return `401 UNAUTHENTICATED`, invalid login details return `401 INVALID_CREDENTIALS`, and an authenticated principal without the required role returns `403 FORBIDDEN`.

## Validation

```text
go test ./...
go vet ./...
go build ./cmd/api
pnpm typecheck:frontend
pnpm test:frontend
pnpm build:frontend
```

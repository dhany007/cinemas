# Cinema Ticketing System Architecture

## Scope

The system lets customers browse films and showtimes, select seats, create an order, pay, receive a QR e-ticket, and lets staff check in tickets. The MVP supports one cinema operator, assigned seats, one currency, and one asynchronous payment gateway.

Multi-tenancy, complex promotions, loyalty programs, native mobile applications, and self-service refunds are out of scope for the MVP. The payment-gateway webhook—not the browser redirect—is the authoritative payment signal.

## System Overview

```text
Browser
  │
  ▼
Next.js + TypeScript
  ├─ customer booking UI
  └─ staff/admin UI
  │ HTTPS JSON API (/v1)
  ▼
Go + Echo API
  ├─ identity and authorization
  ├─ catalog (cinema, studio, movie)
  ├─ scheduling (showtime)
  ├─ seat inventory and reservation holds
  ├─ order and payment orchestration
  ├─ ticket issuance and check-in
  └─ transactional outbox worker
  │
  ├──────────► PostgreSQL: transactional source of truth
  ├──────────► payment gateway: adapter + signed webhook
  └──────────► email/SMS/push provider: outbox-driven delivery
```

The API and worker are stateless processes. Echo handlers translate HTTP only; application services own use cases and transactions; repositories own SQL; and domain packages own state-transition rules. The stack decision is documented in [ADR-0001](adr/0001-go-echo-nextjs-postgresql-modular-monolith.md).

## Module Boundaries

| Module | Responsibility |
| --- | --- |
| `identity` | Customer, staff, and administrator identity and roles. |
| `catalog` | Cinemas, studios, seat layouts, and movie metadata. |
| `scheduling` | Showtimes, prices, and seat inventory creation. |
| `seatinventory` | Availability lookup, holds, expiry, and final allocation. |
| `orders` | Checkout lifecycle, ownership, and idempotency keys. |
| `payments` | Payment intents, webhook verification, event deduplication, and payment state. |
| `tickets` | QR ticket issuance and staff check-in. |
| `outbox` | Notifications and reconciliation after a committed transaction. |

## Data Model

| Table | Key relationships and purpose |
| --- | --- |
| `users` | Customer/staff/admin identity; unique email. |
| `user_roles` | FK `user_id`; unique `(user_id, role)`. |
| `cinemas` | A cinema location. |
| `studios` | FK `cinema_id`; a screening room within a cinema. |
| `seats` | FK `studio_id`; unique `(studio_id, row_label, seat_number)` for the physical layout. |
| `movies` | Movie metadata. |
| `showtimes` | FK `movie_id`, `studio_id`; schedule and base price. |
| `showtime_seats` | FK `showtime_id`, `seat_id`; unique `(showtime_id, seat_id)`; inventory, price, state, hold owner, and expiry. |
| `orders` | FK `user_id`, `showtime_id`; unique booking code and `(user_id, idempotency_key)`. |
| `order_items` | FK `order_id`, `showtime_seat_id`; price snapshot; unique `(order_id, showtime_seat_id)`. |
| `payments` | FK `order_id`; unique `(provider, provider_reference)`. |
| `payment_webhook_events` | Unique `(provider, provider_event_id)` for duplicate or reordered webhooks. |
| `tickets` | FK `order_id`; unique FK `order_item_id`; unique ticket code. |
| `outbox_events` | Post-commit event for an idempotent notification or reconciliation action. |
| `audit_events` | Actor and state-transition audit trail. |

Use foreign keys, status check constraints, parameterized SQL, and indexes for every queried or locked foreign key. Primary indexes cover `showtime_id` for the seat map, state and `hold_expires_at` for expiry processing, orders by user, and webhook events by provider and event ID.

## State Model

```text
Order:   PENDING_PAYMENT → PAID | EXPIRED | CANCELLED | PAYMENT_FAILED
Seat:    AVAILABLE → HELD → SOLD
                      └─ expiry/cancel → AVAILABLE
Payment: PENDING → SUCCEEDED | FAILED | EXPIRED | REFUND_PENDING | REFUNDED
Ticket:  ISSUED → USED | VOID
```

## Seat Reservation and Payment Flow

1. `POST /v1/orders` receives a `showtime_id`, selected `seat_ids`, and an `Idempotency-Key` header.
2. Sort `seat_ids`. In one short PostgreSQL transaction, lock matching `showtime_seats` rows with `SELECT ... FOR UPDATE` in a stable order.
3. Every requested seat must exist and be `AVAILABLE`, or be `HELD` with an expired hold. Otherwise, return a conflict without creating a partial reservation.
4. Create the `PENDING_PAYMENT` order and `order_items`, then mark selected seats `HELD` with `hold_order_id` and `hold_expires_at` (ten minutes by default). Commit before calling the payment provider.
5. Create the payment intent outside the database transaction. Retry only when the provider supports an idempotency key.
6. A verified webhook first inserts or deduplicates the event, then locks the order and its seats. If the order is still valid, one transaction records payment success, marks seats `SOLD`, marks the order `PAID`, creates tickets and audit events, and enqueues a delivery event.
7. An expiry worker claims expired holds and orders in bounded batches with `FOR UPDATE SKIP LOCKED`, releases seats, and marks orders `EXPIRED`. The read path treats a `HELD` seat past `hold_expires_at` as inactive.
8. If expiry wins the race before a successful payment webhook, record the payment as `REFUND_PENDING`; do not reclaim a seat that may already be sold. The outbox starts refund/manual review and sends a notification.

External calls must not run inside a seat transaction. Do not blindly retry booking, payment finalization, or webhook acknowledgement; every retry must be bounded and idempotency-protected.

## API Contract

OpenAPI is the contract source of truth, and every route is versioned under `/v1`.

| Route | Authorization | Contract |
| --- | --- | --- |
| `GET /v1/movies` | Public | Movie list; use keyset pagination when needed. |
| `GET /v1/showtimes/{showtimeId}/seats` | Public | Seat map, price, and availability; never expose a hold or order owner. |
| `POST /v1/orders` | Customer | Creates a hold; `Idempotency-Key` is required. |
| `GET /v1/orders/{orderId}` | Owner/admin | Returns the order, expiry, payment, and issued tickets. |
| `POST /v1/orders/{orderId}/payment-intents` | Owner | Starts payment through the provider adapter. |
| `POST /v1/webhooks/payments/{provider}` | Provider only | Verifies the signature and processes payment finalization idempotently. |
| `GET /v1/tickets/{ticketCode}` | Owner/staff | Returns limited ticket detail. |
| `POST /v1/tickets/{ticketCode}/check-in` | Staff/admin | Atomic `ISSUED → USED`; repeated scans conflict. |
| Admin catalog routes | Admin | Manages cinemas, studios, movies, and showtimes. |

Use a stable error envelope such as `{ "code": "SEAT_UNAVAILABLE", "message": "...", "request_id": "..." }`. Use `400` for malformed input, `401` for unauthenticated access, `403` for unauthorized access, `404` for a missing resource that may be revealed, `409` for seat/state/idempotency/check-in conflicts, `422` for business validation, and `5xx` for internal/provider failures.

## Security and Operations

- Check ownership before reading an order or ticket; only staff and administrators may check in tickets or manage the catalog.
- Verify webhook signatures. Never log passwords, tokens, authorization headers, raw QR secrets, payment payloads, or full PII.
- QR codes carry an opaque or signed token; the server always verifies the current ticket state when scanning.
- Apply rate limits to hold/order routes and webhooks when supported by the gateway.
- Structured logs include request/correlation ID, operation, route template, bounded provider name, outcome, error class, and duration.
- Low-cardinality metrics include booking holds/conflicts, webhook outcomes, ticket issuance, outbox completion/backlog, and transaction latency. Alert on webhook failures, paid orders without tickets, old outbox events, database-pool saturation, and backup/PITR failures.

## Migration and Rollout

- Every schema change uses an immutable SQL migration. Never edit an already-deployed migration or use automatic schema synchronization.
- The initial greenfield migration creates schema and indexes. Once traffic exists, use expand–migrate–contract changes.
- Use concurrent indexes and `NOT VALID` constraints for large tables. Backfills must be separate, batched, resumable, and idempotent workers.
- Run the API and worker with least-privilege database roles; use a separate migration role.
- Before production, run migration smoke tests on representative data, inspect query plans and locking, run concurrency tests, and complete backup and restore drills.

## Implementation Sequence

1. Scaffold `apps/web`, `services/api`, migrations, local Docker development, configuration validation, health endpoints, logging, and the OpenAPI skeleton.
2. Implement catalog/showtime/seat-inventory, user-role, order/payment/ticket, audit, and outbox schema. Materialize `showtime_seats` when a showtime is created.
3. Implement catalog/showtime APIs and administrator authorization; reject schedule overlap and seat-layout changes on active showtimes.
4. Implement public catalog and seat-map read APIs.
5. Implement transactional order holds, expiry, idempotency, error mapping, and contention tests.
6. Implement the payment adapter, payment intent, signed and deduplicated webhooks, and paid-order finalization.
7. Implement QR tickets, outbox notification/reconciliation, the refund-pending path, and atomic check-in.
8. Add CI, migration/contract checks, concurrency and load tests, dashboards, alerts, and staged rollout.

## Verification

- Unit-test state transitions, validation, idempotency, expiry, late payment, QR verification, and HTTP error mapping.
- Use PostgreSQL integration tests to race two orders for the same seat, race expiry against webhook processing, verify rollback removes a hold, verify duplicate/reordered webhooks, and prove concurrent check-in succeeds only once.
- Add contract tests for `401`, `403`, `404`, `409`, and `422`, plus ownership and role authorization.
- After scaffolding, run `go test ./...`, `go vet ./...`, `go build ./cmd/api`, `pnpm lint`, `pnpm typecheck`, `pnpm test`, and a migration smoke test with ephemeral PostgreSQL.
- Manually test two browser sessions selecting the same seat, a repeated webhook, payment at the hold-expiry boundary, and two scans of the same ticket.

## Open Decisions

1. Payment gateway, currency, tax/service-fee calculation, and refund SLA.
2. Organization OIDC provider versus local email/password authentication.
3. Hold duration and the final late-payment policy.
4. Whether seat-type, time-based, member, or promotional pricing is required in the MVP.
5. Whether customer cancellation/refund is required in the MVP.

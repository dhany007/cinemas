# Cinema Product Roadmap

This checklist is the single delivery sequence for the cinema platform. A feature is started only when its dependency stage is complete or an explicit exception is recorded here. The product has two application roles only: `CUSTOMER` and `ADMIN`.

## Working Rules

- Keep work within one unchecked checklist item or a tightly related subgroup.
- Update this file in the same pull request as a completed roadmap item.
- Do not introduce a `STAFF` role or staff-facing workflow.
- Prefer a complete vertical slice—schema, API, authorization, tests, documentation, and observability—over partially building several features.
- Mark an item complete only after its listed acceptance criteria and validation are complete.

## Current Baseline

- [x] PostgreSQL schema for cinemas, studios, seats, movies, showtimes, orders, payments, tickets, audit events, and outbox events.
- [x] Public movie catalog, movie showtimes, and showtime seat map APIs.
- [x] Transactional seat holds with idempotency.
- [x] Local fake-payment completion, seat sale, and ticket issuance.
- [x] Customer registration/login and protected admin bootstrap using password hashes and access tokens.
- [x] Customer ownership enforcement for checkout and fake payment.
- [x] Remove the `STAFF` role from the current role constraint through a forward migration.
- [x] Run all migrations, including authentication migrations, against a disposable PostgreSQL database.
- [x] Add CI and a documented local development environment.

## Stage 1 — Platform Safety and Delivery Foundation

**Goal:** make the existing backend reproducible and safe to change.

- [x] Provide local Docker Compose for PostgreSQL and the API.
- [x] Add a documented migration command and an ephemeral PostgreSQL migration smoke test.
- [x] Add CI for formatting, linting, tests, build, and migration validation.
- [x] Add an API error/response contract document or OpenAPI specification.
- [x] Add rate limiting for registration, login, order-hold, and payment routes.
- [x] Add structured request logging without passwords, tokens, or authorization headers.

**Done when:** a new developer can run the API and migrations locally, and every pull request executes the same automated validation.

## Stage 2 — Admin Cinema and Catalog Management

**Goal:** allow an administrator to configure what customers can buy.

- [x] Add authenticated admin-only CRUD for cinemas.
- [x] Add authenticated admin-only CRUD for studios.
- [x] Add authenticated admin-only CRUD for seat layouts.
- [ ] Add authenticated admin-only CRUD for movies.
- [ ] Add authenticated admin-only CRUD for showtimes and materialize `showtime_seats`.
- [ ] Reject overlapping showtimes in one studio.
- [ ] Prevent seat-layout changes that would invalidate active or historical showtimes.
- [ ] Record administrator actions in `audit_events`.

**Done when:** an admin can create a complete cinema, studio, seat layout, movie, and saleable showtime entirely through the API, with authorization and conflict tests.

## Stage 3 — Customer Booking Journey Completion

**Goal:** complete the customer-side order lifecycle around the existing hold.

- [ ] Add `GET /v1/orders/{orderId}` with owner-only access.
- [ ] Add `GET /v1/orders` for the authenticated customer's order history.
- [ ] Return ordered items, showtime summary, payment state, ticket state, and hold expiry without leaking another customer's data.
- [ ] Implement pending-hold expiry processing that marks orders expired and releases seats.
- [ ] Add cancellation rules for unpaid holds, if still allowed before expiry.
- [ ] Add PostgreSQL concurrency tests for duplicate requests, competing seat selections, and hold expiry.

**Done when:** customers can view the result of every checkout attempt, expired holds release inventory, and concurrent access has database-backed tests.

## Stage 4 — Production Payment Integration

**Goal:** replace the local fake provider with a real, asynchronous, recoverable payment flow.

- [ ] Define the payment provider adapter interface and provider configuration.
- [ ] Create real payment intents using an idempotency key.
- [ ] Add signed webhook verification and replay-window validation.
- [ ] Deduplicate provider events in `payment_webhook_events`.
- [ ] Finalize paid orders, seats, tickets, and audit events atomically.
- [ ] Handle failed, expired, late, duplicated, and out-of-order payment events.
- [ ] Define refund and manual-review rules for a payment received after hold expiry.
- [ ] Retain the fake provider only for local development and tests.

**Done when:** the provider webhook—not a browser redirect—is the only authority that changes an order to paid, and every payment event is idempotent.

## Stage 5 — Tickets and Customer Notifications

**Goal:** deliver usable tickets after a successful payment.

- [ ] Add owner-only ticket retrieval for a paid order.
- [ ] Generate opaque, non-guessable QR ticket tokens; never expose token hashes.
- [ ] Add ticket delivery through the transactional outbox.
- [ ] Implement retryable notification delivery and reconciliation for unprocessed events.
- [ ] Add customer notification preferences only if a delivery channel requires them.

**Done when:** every paid order produces tickets exactly once, delivery is recoverable after failures, and ticket data is visible only to its owner or an admin.

## Stage 6 — Admin Ticket Validation and Operations

**Goal:** let administrators operate the cinema safely on show day.

- [ ] Add admin-only ticket lookup with minimal customer data.
- [ ] Add admin-only atomic ticket check-in (`ISSUED` to `USED`).
- [ ] Return a conflict for repeated QR scans without changing the first check-in record.
- [ ] Add operational views/APIs for expiring holds, payment exceptions, and notification failures.
- [ ] Add audit events for check-in and privileged operational actions.

**Done when:** concurrent scans can consume a ticket only once, administrators can resolve exceptions, and all privileged actions are auditable.

## Stage 7 — Next.js Applications

**Goal:** expose the completed backend journeys through two focused web experiences.

- [ ] Build the customer movie, showtime, seat-selection, checkout, payment-status, order-history, and ticket views.
- [ ] Build the admin cinema, seat-layout, movie, showtime, and ticket-validation views.
- [ ] Integrate authenticated API calls without exposing server secrets to the browser.
- [ ] Add accessible loading, error, empty, and expired-hold states.
- [ ] Add frontend contract tests for the API journeys already delivered by the backend.

**Done when:** each UI is limited to its role, all customer and admin journeys use the protected API, and the interface handles common failures clearly.

## Stage 8 — Production Readiness

**Goal:** deploy safely and operate the platform with confidence.

- [ ] Add deployment configuration, environment validation, and a rollback runbook.
- [ ] Use a managed secret store for database, JWT, bootstrap, and payment-provider secrets.
- [ ] Add backups, point-in-time recovery verification, and restore drills.
- [ ] Add metrics, dashboards, and alerts for booking conflicts, payment outcomes, webhook failures, ticket issuance, outbox backlog, and database saturation.
- [ ] Run load and contention testing for popular showtimes.
- [ ] Perform security review for authentication, authorization, payment webhooks, QR tokens, and administrative actions.
- [ ] Run a staged release with smoke tests and production rollback criteria.

**Done when:** the service has an approved deployment path, operational monitoring, tested recovery, and production sign-off.

## Deferred Until Explicitly Prioritized

- Promotions, memberships, loyalty points, and dynamic pricing.
- Multiple cinema operators or tenant isolation.
- Customer self-service refunds.
- Native mobile applications.
- Additional roles beyond `CUSTOMER` and `ADMIN`.

# ADR-0001: Adopt Go/Echo, Next.js, and a PostgreSQL Modular Monolith

**Status:** Proposed

**Date:** 2026-08-26

## Context

A cinema ticketing system handles monetary transactions and a highly contested resource: one seat for one showtime may only be sold once. The system needs a durable API boundary for the customer web application, administrator tools, and future clients. There is no current evidence of service boundaries or team ownership that would justify microservices at the start.

## Decision

- Use the stable Go version available at implementation time and Echo v4 for the versioned REST API under `/v1`.
- Use Next.js, TypeScript, and App Router for customer and administrator web applications.
- Use PostgreSQL 16+ or an equivalent managed PostgreSQL service as the transactional system of record.
- Use `numeric(12,2)` for money and `timestamptz` for application timestamps.
- Use an immutable Go SQL migration tool, preferably `goose`, and a PostgreSQL driver that supports `context.Context`.
- Use a PostgreSQL transactional outbox and a Go worker for idempotent asynchronous work such as ticket delivery and paid-order reconciliation.
- Access payment gateways through an interface/adapter. Webhooks must be signature-verified, durable, and deduplicated by `(provider, provider_event_id)`.
- Run API and worker as stateless containers/processes with managed PostgreSQL, backups, and point-in-time recovery.
- Do not use Redis or a message broker in the MVP. Redis may be added later for demonstrated caching or rate-limiting needs, but it must never be the source of truth for seat inventory.

## Rationale

PostgreSQL provides the transactions, unique constraints, and row locking required to prevent duplicate seat sales. Go/Echo and Next.js match the preferred stack while establishing a clear API/client boundary. A modular monolith keeps deployment, debugging, and observability straightforward without sacrificing domain modularity.

## Consequences

### Positive

- ACID transactions and deterministic row locks prevent duplicate sales without UI-only locking or distributed transactions.
- A single domain deployment accelerates the MVP and limits operational overhead.
- The `/v1` API can serve a mobile application or other clients later.
- The transactional outbox prevents ticket delivery and reconciliation work from being lost after a payment succeeds.

### Negative

- Package boundaries in the modular monolith require discipline to avoid an unstructured monolith.
- The outbox worker, PostgreSQL pool, and paid-order reconciliation must be monitored from the beginning.
- Checkout throughput for the same seat/showtime is limited by locks; this is a deliberate correctness trade-off.

### Follow-up

- If metrics show high seat-map read load, evaluate a Redis cache with post-commit invalidation.
- If event volume or team ownership genuinely grows, evaluate a broker or service extraction based on the established module boundaries rather than initial assumptions.

## Alternatives Considered

| Alternative | Decision | Reason |
| --- | --- | --- |
| Microservices and a message broker from day one | Rejected | Adds distributed consistency, deployment, tracing, and failure modes without proven scale or organization needs. |
| Redis as the only seat lock/inventory | Rejected | TTL, eviction, and failover make recovery and duplicate-sale prevention insufficiently durable. |
| PostgreSQL-only holds | Accepted | Provides the smallest component count and reliable row-lock correctness for the MVP. |
| Next.js as the only backend | Rejected | Does not create a strong reusable domain API and does not meet the Go/Echo preference. |
| NoSQL as the primary database | Rejected | Relational data, uniqueness, auditability, and booking transactions fit PostgreSQL better. |

## Implementation Guardrails

- PostgreSQL is the only authority for `AVAILABLE`, `HELD`, and `SOLD` seat state.
- Never call a payment provider inside a database transaction.
- Process every payment webhook idempotently; browser redirects must not change order state.
- Keep all migrations immutable; use a forward fix or feature flag for production recovery when needed.
- API errors require stable machine-readable codes and must not expose internal details or PII.

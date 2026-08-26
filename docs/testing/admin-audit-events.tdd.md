# TDD Evidence - Administrator Audit Events

## Source Plan

- `docs/roadmap.md`, Stage 2: record administrator actions in `audit_events`.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
|---|-----------|-------------|-----------|--------------|----------------|
| 1 | Cinema create, update, and delete operations record the authenticated administrator as actor. | `internal/httpapi/server_test.go:TestServerAdminCinemaCRUD` | HTTP integration | Not applicable: the existing CRUD implementation already recorded the three events before this roadmap item was reconciled. | The test verifies three events after the API lifecycle. |
| 2 | Seat, movie, and showtime mutations record `CREATE`, `UPDATE`, and `DELETE` events with their resource type. | `internal/admin/service_test.go:TestServiceManagesSeatLayoutAndRecordsAuditEvents`, `TestServiceManagesMovieAndRecordsAuditEvents`, and `TestServiceManagesShowtimeAndMaterializesSeats` | Domain unit | Not applicable: auditing was implemented atomically with each CRUD feature. | The tests assert matching entity types and lifecycle actions. |
| 3 | PostgreSQL persists an audit event in the same transaction as every supported admin mutation. | `internal/postgres/admin_repository.go` | Repository inspection | Not applicable: the persistence implementation already existed. | All 15 create/update/delete paths call `insertCinemaAuditEvent` before commit; rollback covers a failed audit insert or business mutation. |

## Validation Commands

- `rg -n 'insertCinemaAuditEvent\\(' internal/postgres/admin_repository.go`: verified all 15 cinema, studio, seat, movie, and showtime mutation paths.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./internal/admin ./internal/httpapi -run 'Test(ServerAdminCinemaCRUD|ServiceManagesSeatLayoutAndRecordsAuditEvents|ServiceManagesMovieAndRecordsAuditEvents|ServiceManagesShowtimeAndMaterializesSeats)' -count=1`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go test ./...`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache go vet ./...`: passed (the sandbox emitted a non-fatal module stat-cache write warning after completion).
- `GOCACHE=/private/tmp/cinemas-go-build-cache go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `GOCACHE=/private/tmp/cinemas-go-build-cache GOLANGCI_LINT_CACHE=/private/tmp/cinemas-golangci-cache golangci-lint run -c .golangci.yml ./internal/admin ./internal/httpapi ./internal/postgres`: passed with `0 issues`.
- `git diff --check`: passed.

## Notes

- `audit_events` stores actor user ID, entity type, entity ID, action, and database-generated creation time.
- The actor comes from the authenticated admin identity; failed mutations do not commit an audit event because repository writes are transactional.
- No audit-reading endpoint is added here: the roadmap requirement is recording, not exposing an audit-log API.

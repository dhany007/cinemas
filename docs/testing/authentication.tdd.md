# TDD Evidence - Authentication and Authorization

## Source Plan

- User request: implement authentication for administrators and users; the application has no staff role.

## Testable Guarantees

| # | Guarantee | Test target | Test type | RED evidence | GREEN evidence |
| --- | --- | --- | --- | --- | --- |
| 1 | Customer registration creates a customer identity, issues a usable token, and permits password login. | `internal/auth/service_test.go:TestServiceRegisterThenLogin` | Unit | `go test ./internal/auth -run 'TestService(RegisterThenLogin|LoginRejectsIncorrectPassword|RegisterAdminCreatesAdminOnly)' -count=1` failed with undefined auth service and types. | The same command passed after the auth service, bcrypt hashing, JWT signer, and memory repository were added. |
| 2 | Invalid passwords are rejected without exposing whether the email exists, and only one bootstrap admin can be created. | `internal/auth/service_test.go:TestServiceLoginRejectsIncorrectPassword`; `TestServiceRegisterAdminCreatesAdminOnly` | Unit | Included in the missing-auth API RED failure above. | The focused auth test command passed. |
| 3 | Checkout uses the bearer-token identity, not a body-supplied `user_id`, and missing tokens return a stable `401`. | `internal/httpapi/server_test.go:TestServerRegistersCustomerAndUsesTokenIdentityForCheckout`; `TestServerRejectsCheckoutWithoutAccessToken` | API | `go test ./internal/httpapi -run 'TestServer(RegistersCustomerAndUsesTokenIdentityForCheckout|RejectsCheckoutWithoutAccessToken)' -count=1` failed because `NewServerWithAuth` did not exist. | The same command passed after authentication routes and role middleware were added. |
| 4 | Admin bootstrap rejects an invalid environment token and a customer cannot pay another customer's order. | `internal/httpapi/server_test.go:TestServerRejectsAdminBootstrapWithoutConfiguredToken`; `internal/payments/service_test.go:TestServiceCreateFakePaymentDoesNotExposeAnotherCustomersOrder` | API/unit | The bootstrap scenario was covered by the missing-auth HTTP RED failure; payment ownership is a regression test added with the protected payment contract. | Full test suite passed after authentication and payment ownership enforcement were integrated. |
| 5 | Startup rejects a short JWT secret and accepts an explicit token TTL. | `cmd/api/main_test.go:TestLoadAuthenticationConfig`; `TestLoadAuthenticationConfigRejectsShortJWTSecret` | Unit | Added as validation for the startup configuration contract. | `go test ./...` passed. |

## Validation Commands

- `go test ./...`: passed.
- `go vet ./...`: passed.
- `go build -o /private/tmp/cinemas-api ./cmd/api`: passed.
- `golangci-lint config verify -c .golangci.yml`: passed.
- `golangci-lint run -c .golangci.yml ./...`: passed with `0 issues`.
- `git diff --check`: passed.

## Coverage / Gaps

- Unit and HTTP handler coverage prove registration, login, access-token validation, protected checkout, bootstrap rejection, and payment ownership.
- PostgreSQL migration execution has not been run against a disposable database in this change; the migration is a forward-only nullable password-hash expansion followed by the explicit role constraint change.

## Notes

- `AUTH_JWT_SECRET` is required at startup and must be at least 32 bytes. `AUTH_ADMIN_BOOTSTRAP_TOKEN` is optional but required to use the bootstrap-admin endpoint.
- The initial `users` migration is immutable. Migration `000003` adds a nullable `password_hash` to preserve compatibility with any existing rows; login rejects rows with no password hash.
- Migration `000004` removes the legacy `STAFF` value from the database role constraint. It will fail safely if an existing deployment still contains a staff role and requires a deliberate data migration before retrying.

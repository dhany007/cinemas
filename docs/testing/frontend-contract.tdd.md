# TDD Evidence — Next.js Applications

## Source plan

- `docs/roadmap.md`, Stage 7: focused customer and administrator Next.js applications.

## Testable guarantees

| Guarantee | Test target | RED evidence | GREEN evidence |
|---|---|---|---|
| Each browser journey is based on documented backend paths. | `tests/frontend-contract.test.mjs` | The initial test could not read either application BFF client because it did not exist. | The test checks all public/customer/admin paths used by the frontend against `openapi/openapi.yaml`. |
| Browser code does not read the backend URL or construct the authorization header. | `tests/frontend-contract.test.mjs` | The same missing-client failure prevented an implementation from passing. | Both browser API clients use only `/api/backend`; the test rejects `API_BASE_URL` and `Authorization` in those files. |
| Login does not return a bearer token to browser code. | `tests/frontend-contract.test.mjs` | The session route did not exist before the application implementation. | The test requires the session response to include only the user and a HTTP-only cookie. |

## Validation commands

```text
pnpm typecheck:frontend
pnpm test:frontend
pnpm build:frontend
```

All commands passed after the customer query-string pages were wrapped in `Suspense` for production prerendering.

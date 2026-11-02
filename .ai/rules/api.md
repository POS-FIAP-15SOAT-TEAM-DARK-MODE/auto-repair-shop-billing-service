# API — Billing Service (port 8083)

Base path `/v1`. Monetary values are decimal strings. Swagger: `docs/swagger.yaml` (embedded, served at `/swagger/index.html` and `/swagger.yaml`).

```
GET  /v1/budgets/:serviceOrderId      ADMIN, ATTENDANT, MECHANIC, CUSTOMER (owner only)
GET  /v1/payments/:serviceOrderId     ADMIN, ATTENDANT
POST /v1/payments/:id/confirm         ADMIN, ATTENDANT   204 | 401 | 403 | 404 | 409
POST /v1/payments/:id/reject          ADMIN, ATTENDANT   204 | 401 | 403 | 404 | 409
GET  /ping, /metrics, /swagger/*     public
```

## Errors
- Middleware (401 / 403): `{"code": 401|403, "error": "Unauthorized" | "Token expired" | "Forbidden"}`.
- Handler errors: `{"code": <status>, "error": "<message>"}` for 404, 409 and generic 500. Only domain sentinels reach the body (`budget not found`, `payment not found`, `invalid payment status transition`); infrastructure failures are answered as `internal server error`.
- Validation errors use `{"code": 400, "errors": ["<message>", ...]}`. No route in this service accepts a request body today, so this path is reserved for future input.
- The cause of a 422 and of a 409 that is not an invalid transition is logged; the body never contains it.

## Rules
- Route roles are declared in `internal/app/routing/routing.go` with `middleware.Auth(...)`.
- Confirm/reject emit `PaymentApproved` / `PaymentFailed` through the outbox in the same transaction.
- No `/internal` routes in this service: the gateway does not route them.
- Every change to a route or response must update `docs/swagger.yaml` in the same commit.
- `X-Request-Id` on confirm/reject is copied to the emitted events (generated when absent).

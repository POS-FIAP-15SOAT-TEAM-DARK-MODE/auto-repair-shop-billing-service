# API — Billing Service (port 8083)

Base path `/v1`. Monetary values are decimal strings. Errors: `{"code", "error", "errors"}` (see `internal/pkg/web`).

```
GET  /v1/budgets/:serviceOrderId      ADMIN, ATTENDANT, MECHANIC, CUSTOMER (owner only)
GET  /v1/payments/:serviceOrderId     ADMIN, ATTENDANT
POST /v1/payments/:id/confirm         ADMIN, ATTENDANT   204 | 404 | 409
POST /v1/payments/:id/reject          ADMIN, ATTENDANT   204 | 404 | 409
GET  /ping, /metrics, /swagger/*      public
```

- Route roles are declared in `internal/app/routing/routing.go` with `middleware.Auth(...)`.
- Confirm/reject emit `PaymentApproved`/`PaymentFailed` through the outbox in the same transaction.
- No `/internal` routes in this service. Swagger: `docs/swagger.yaml` (embedded).

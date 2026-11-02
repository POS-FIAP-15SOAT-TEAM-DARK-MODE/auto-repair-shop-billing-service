# Security

## JWT
- All protected endpoints require `Authorization: Bearer <JWT>` — return 401 if missing/invalid.
- Public endpoints (no JWT): the ping endpoint and, only in `mercadopago` mode, `POST /v1/webhooks/mercadopago`, which is authenticated by its `x-signature` header (see `payment-gateway.md`).
- JWT payload must include: `user_id` and `roles` (array). CUSTOMER access to a budget is checked against `user_id` == `budget.customer_user_id`.
- Auth middleware must not make extra DB calls — rely entirely on the JWT payload.
- Expiry configurable via `JWT_EXPIRES_IN` env var.

## RBAC Enforcement
- Map roles from the JWT payload; reject out-of-scope access with 403.
- `ADMIN` is exclusive — a user with ADMIN cannot have other roles.
- Role constants and groups live in `internal/domain/roles.go`.
- Groups in `internal/pkg/auth/roles.go`: `AttendantRoles`, `MechanicRoles`, `CustomerRoles`, `AttendantAndMechanicRoles`.

## Passwords
- bcrypt, default cost factor 10 (`bcrypt.DefaultCost`), configurable via `BCRYPT_COST` env var.
- Payment outcomes are decided only by the payment provider or an authenticated attendant (`/v1/payments/:id/confirm|reject`).
- Never log or return password hashes.

## PII — Never Expose or Log
CPF, CNPJ, passwords, and JWT tokens must never appear in:
- HTTP response bodies or headers.
- Log entries (centralized redaction handled in `internal/pkg/logger/logger.go`).
- Error messages returned to clients.

Avoid `zap.Any` for domain/request objects that may contain PII — use explicit safe fields instead.

## Input Sanitization
- Validate all user input at handler layer before passing to service.
- Use domain constructors (`budget.NewBudget`, `payment.Payment.Transition`) — never trust raw strings.
- Never expose stack traces or internal error details in production responses.

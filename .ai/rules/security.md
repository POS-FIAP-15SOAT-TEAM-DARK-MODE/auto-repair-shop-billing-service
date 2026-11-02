# Security

This service issues no tokens and stores no credentials. Tokens are issued by the auth lambda (`auto-repair-shop-lambda-auth`); billing only verifies the JWT it receives.

## JWT
- Protected endpoints require `Authorization: Bearer <JWT>`. Missing or invalid token → 401 (`internal/app/middleware/auth.go`).
- Signature: HS256 with `JWT_SECRET`. The code falls back to a placeholder when the variable is empty (`internal/pkg/auth/jwt.go`); production must always set it.
- Claims used: `user_id` and `roles` (array). The customer check compares `user_id` with `budget.customer_user_id`.
- The middleware makes no DB call; it relies only on the JWT payload.
- Public endpoints: `/ping`, `/metrics`, `/swagger/*`, `/swagger.yaml` and, only in `mercadopago` mode, `POST /v1/webhooks/mercadopago`, which is authenticated by its `x-signature` header (see `payment-gateway.md`).

## RBAC Enforcement
- Each route declares its roles with `middleware.Auth(...)` in `internal/app/routing/routing.go`. A token without any listed role → 403.
- Role constants: `internal/pkg/auth/roles.go` (`ADMIN`, `ATTENDANT`, `MECHANIC`, `CUSTOMER`). Groups: `AttendantRoles` (ATTENDANT, ADMIN) and `AttendantAndMechanicRoles` (ATTENDANT, MECHANIC, ADMIN).
- `ADMIN` is listed explicitly in every route that allows it. Nothing in the code makes ADMIN exclusive.
- A `CUSTOMER` reading a budget only sees its own (`claims.UserId == budget.CustomerUserID`); other budgets answer 404.

## Payments
- Payment outcomes are decided only by the payment provider (`PAYMENT_MODE`) or by an authenticated attendant (`POST /v1/payments/:id/confirm|reject`).

## PII — Never Expose or Log
CPF, CNPJ, passwords, JWT tokens and the `Authorization` header must never appear in:
- HTTP response bodies or headers.
- Log entries. The logger redacts sensitive keys (`isSensitiveKey` in `internal/pkg/logger/logger.go`), but do not rely on it: pass explicit safe fields.
- Error messages returned to clients.

Avoid `zap.Any` for domain or request objects that may contain PII.

## Input Sanitization
- Validate all user input at the handler/controller layer before it reaches the service. Path params are read from the context and used as opaque ids.
- Use domain constructors (`budget.NewBudget`, `payment.Payment.Transition`) for business validation; never trust raw strings in services or domain.
- Never expose stack traces or internal error details in production responses (`internal/pkg/web`).

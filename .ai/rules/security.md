# Security

## JWT
- All protected endpoints require `Authorization: Bearer <JWT>` — return 401 if missing/invalid.
- Public endpoints (no auth): `POST /v1/auth/login` and the ping endpoint only.
- JWT payload must include: `roles` (array) and `customerId` (when applicable).
- Auth middleware must not make extra DB calls — rely entirely on the JWT payload.
- Expiry configurable via `JWT_EXPIRES_IN` env var.

## RBAC Enforcement
- Map roles from the JWT payload; reject out-of-scope access with 403.
- `ADMIN` is exclusive — a user with ADMIN cannot have other roles.
- Role constants and groups live in `internal/domain/roles.go`.
- Groups: `AttendantRoles`, `MechanicRoles`, `CustomerRoles`, `AttendantAndMechanicRoles`.

## Passwords
- bcrypt, default cost factor 10 (`bcrypt.DefaultCost`), configurable via `BCRYPT_COST` env var.
- Default customer password = their CPF or CNPJ.
- Never log or return password hashes.

## PII — Never Expose or Log
CPF, CNPJ, passwords, and JWT tokens must never appear in:
- HTTP response bodies or headers.
- Log entries (centralized redaction handled in `internal/pkg/logger/logger.go`).
- Error messages returned to clients.

Avoid `zap.Any` for domain/request objects that may contain PII — use explicit safe fields instead.

## Input Sanitization
- Validate all user input at handler layer before passing to service.
- Use domain validation methods (`customer.Validate()`, etc.) — never trust raw strings.
- Never expose stack traces or internal error details in production responses.

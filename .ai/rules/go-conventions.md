# Go Conventions

## Tech Stack
Go 1.27 · Gin · PostgreSQL 15 · `lib/pq` (raw SQL, no ORM) · `golang-migrate` (CLI, migrations) · `golang-jwt/jwt/v5` · Zap · Testify · `go-sqlmock` · `shopspring/decimal` · `google/uuid` · `oklog/ulid/v2` · AWS SDK v2 (SQS, SNS) · Prometheus client · swaggo (Swagger UI)

## ID Types
- `github.com/google/uuid` for entity ids and `message_id`s (`internal/pkg/id`).
- `oklog/ulid` is available through `id.Generator.NewULID()`. Service order ids arrive in the messages and are stored as `VARCHAR(26)`.

## Monetary Values
Always use `shopspring/decimal.Decimal`, never `float64` or `float32`. DB column type: `NUMERIC(10,2)`. JSON carries decimal strings (`"221.00"`).

## No Framework Leakage
- Domain packages (`internal/budget`, `internal/payment`): no imports from `gin`, `lib/pq`, `aws` or `internal/app`. The only infrastructure import allowed is `internal/pkg/db`, for the `db.Executor` type used by the repository interfaces.
- Service (`internal/billing/service.go`): may use `database/sql` transactions and `internal/pkg/messaging`; it must not contain SQL.
- Gin types stay in `internal/app`.

## Test Doubles
No mocks are generated. Tests use hand-written fakes in `*_test.go` (for example `errBudgets` and `errPayments` in `internal/billing/service_test.go`) and the in-memory repositories described below. Do not reintroduce a mock generator without a rule change.

## Error Patterns
- Domain errors: `var ErrXxx = errors.New("...")` in the domain package (`budget.ErrNotFound`, `payment.ErrInvalidTransition`, ...).
- Infrastructure sentinels live in `internal/app/errors.go` (`ErrDataConflict`, `ErrDataViolation`, `ErrInfraConflict`) and `internal/pkg/json/error.go`.
- Services return domain errors. `internal/pkg/web/errors.go` maps them to HTTP codes with `errors.Is` checks inside a `switch`.
- Never expose internal error details in HTTP responses (generic messages for 409/422/5xx).

## In-Memory Implementations
Each repository has `postgres.go` and `in_memory.go` under `internal/<domain>/repository/` (`budget`, `payment`). In-memory variants are used by unit tests only.

## Configuration
Env vars are read with `internal/pkg/env` (`GetString`, `GetInt`, `GetTimeDuration`):
- HTTP: `PORT` (default `8083`).
- Database: `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_HOST`, `POSTGRES_PORT`, `POSTGRES_DB`, `POSTGRES_SSLMODE`.
- Auth: `JWT_SECRET`.
- Payments: `PAYMENT_MODE` (`manual` | `auto-approve` | `auto-reject`).
- Messaging: `BILLING_QUEUE_URL`, `BILLING_EVENTS_TOPIC_ARN`, `AWS_REGION`, `AWS_ENDPOINT_URL`.
- Logs: `LOG_LEVEL`, `SERVICE_NAME`.

The Makefile loads `.env` (`-include .env`) and exports it. Template: `.env.example`.

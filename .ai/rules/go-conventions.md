# Go Conventions

## Tech Stack
Go 1.26 · Gin · PostgreSQL 15 · `lib/pq` (raw SQL, no ORM) · `golang-migrate` · `golang-jwt/jwt/v5` · Zap · Testify · Mockery v2 · `shopspring/decimal` · `google/uuid` · `oklog/ulid/v2`

## ID Types
- `google/uuid` — all entity IDs except service orders.
- `oklog/ulid/v2` — `ServiceOrder` and `ServiceOrderHistory` IDs (sortable by creation time).

## Monetary Values
Always use `shopspring/decimal.Decimal`, never `float64` or `float32`. DB column type: `NUMERIC(10,2)`.

## No Framework Leakage
- Domain structs: zero imports from `gin`, `pq`, or any `infra` package.
- Service layer: only imports domain interfaces and `uow` package.
- Gin types are confined to `infra/handler/`.

## Mock Generation
Mocks live in `internal/domain/mocks/` and are generated via `//go:generate` directives on interface files:
```go
//go:generate go run github.com/vektra/mockery/v2 --name=CustomerRepository
```
Run `make mockgen` to regenerate. Never edit generated mock files manually.

## Error Patterns
- Domain errors: `var ErrXxx = errors.New("...")` in the domain package.
- Services return domain errors; handlers map them to HTTP codes via a type switch.
- Never expose internal error details in HTTP responses.

## In-Memory Implementations
Every domain has both `postgres.go` and `in_memory.go` in `infra/repository/<domain>/`. The in-memory variant is used exclusively for unit tests.

## Configuration
All env vars via `internal/pkg/env/`: `DATABASE_URL`, `JWT_SECRET`, `JWT_EXPIRES_IN`, `BCRYPT_COST`, `PORT` (default `8080`). Makefile reads from `.env` and exports automatically.

# Playbook: Scaffold New Domain

Use this playbook when adding a fully new domain (e.g., `supplier`).

## 1. Domain Model — `internal/domain/<name>.go`
- Define struct with `uuid.UUID` ID and `CreatedAt`/`UpdatedAt` timestamps.
- Add `Validate() error` method.
- Define `<Name>Repository` interface with CRUD methods.
- Add `//go:generate go run github.com/vektra/mockery/v2 --name=<Name>Repository` directive.

## 2. In-Memory Repository — `internal/infra/repository/<name>/in_memory.go`
- Implement `<Name>Repository` with a `map[uuid.UUID]*domain.<Name>`.
- Used exclusively for unit tests.

## 3. Postgres Repository — `internal/infra/repository/<name>/postgres.go`
- Stateless struct: `type repo struct{}`.
- Extract connection: `postgres.GetTransaction(ctx)` (mutating) or `postgres.GetOneTimeTransaction(ctx)` (read-only).
- All queries as raw SQL strings — no ORM.

## 4. Service — `internal/services/<name>/service.go`
- Constructor: `func NewService(uow uow.UoW, repo domain.<Name>Repository) *Service`.
- All business logic here; wrap mutations in `uow.Execute(ctx, ...)`.

## 5. Handler — `internal/infra/handler/<name>.go`
- Parse and validate HTTP input; call service; return response.
- Zero business logic — delegate all decisions to the service.

## 6. Wire — `internal/infra/factory/http.go`
- Instantiate: `repo → service → handler`.
- Register routes with the correct role middleware.

## 7. Migration
Follow `add-migration.md`. Add table, indexes, and FK constraints.

## 8. Tests
- Unit: service layer with in-memory repo + in-memory UoW (`infra/db/in_memory`).
- Integration: postgres repo against a real test DB, rolled back after each test.
- Coverage target: 80% minimum (90% if touches budget, stock, or service order).

## 9. Regenerate Mocks
```bash
make mockgen
```

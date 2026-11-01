# Architecture

## Layer Map

```
HTTP → Handler (infra/handler/*) → Service (internal/services/*) → Repository (infra/repository/*) → PostgreSQL
                                         ↕
                               Domain (internal/domain/*)
```

## Layer Contracts
- **Handler**: parse + validate HTTP input, call service, return response — zero business logic.
- **Service**: all business logic, status enforcement, stock checks, budget recalc.
- **Repository**: all SQL via `lib/pq` — no queries outside repositories.
- **Domain**: framework-free structs + validation methods + service/repository interfaces.

## DI Root
`internal/infra/factory/http.go` — single wiring file: creates DB, repos, services, handlers, passes into `HandlersWrapper`. All constructor injection happens here.

## Repository DB Connection Pattern
Repositories hold no `*sql.DB`. Connection comes from context, injected by UoW:
- **Mutating ops** (inside tx): `postgres.GetTransaction(ctx)` → returns `*sql.Tx` stored by UoW `OnStart` hook.
- **Read-only ops**: `postgres.GetOneTimeTransaction(ctx)` → opens a fresh `*sql.DB` connection.

## Unit of Work
```go
uow.Execute(ctx, func1, func2, ...)
```
UoW begins a transaction, injects it into context, runs all steps, then commits or rolls back. Services must wrap all mutating repository calls in `uow.Execute`.

## Dynamic SQL
Use `db.QueryBuilder(baseQuery)` from `internal/pkg/db/`:
```go
qb := db.QueryBuilder(base)
qb.Add("column =", value)
qb.OrderBy(field, db.ASC)
qb.AddPagination(limit, offset)
query, args := qb.Build()
```

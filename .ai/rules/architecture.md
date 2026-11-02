# Architecture

## Layer Map

```
REST:      HTTP → routing (internal/app/routing) → Controller (internal/billing/controller.go) → Service
Messaging: SQS → Consumer (internal/pkg/messaging/consumer.go) → Service handlers (RegisterHandlers)
Outbox:    Service writes outbox rows in the same tx → Relay (internal/pkg/messaging/relay.go) → SNS
                                  Service (internal/billing/service.go)
                                         ↕
                        Domain (internal/budget, internal/payment)
                                         ↓
                     Repository (internal/{budget,payment}/repository) → PostgreSQL
```

## Layer Contracts
- **Routing / middleware** (`internal/app`): mount routes, role checks (`middleware.Auth`), logging, metrics, recovery. No business logic.
- **Controller** (`internal/billing/controller.go`): read route params and claims, call the service, map to response DTOs. Gin types stay in `internal/app`.
- **Service** (`internal/billing/service.go`): command handlers, status enforcement, budget dedupe, payment decision flow. Opens its own `*sql.Tx` where needed. No SQL.
- **Repository** (`internal/{budget,payment}/repository`): all SQL, using `lib/pq` through `db.Executor`. Holds no `*sql.DB`.
- **Domain** (`internal/budget`, `internal/payment`): structs, constructors, transitions and sentinel errors. No framework imports.

## DI Root
`internal/app/bootstrap/bootstrap.go` builds the DB connection, repositories, provider (`PAYMENT_MODE`), service, controller, HTTP server and messaging workers. `internal/app/container` holds the handler and middleware wrappers passed to routing.

## Repository DB Connection Pattern
Repositories take a `db.Executor` argument (`internal/pkg/db/executor.go`), implemented by `*sql.DB` and `*sql.Tx`. The caller chooses the connection:
- **Writes inside a command**: the service passes the `*sql.Tx` it opened (consumer dedupe + effect + outbox + commit).
- **REST reads**: the service passes its `*sql.DB`.

## Transactions
- Consumer (`internal/pkg/messaging/consumer.go`): one transaction per message for dedupe (`processed_message`), the command effect and the outbox write.
- REST decisions (`Confirm` / `Reject`, `service.decide`): the service opens its own `*sql.Tx`.
- Outbox relay (`relay.go`): selects a pending batch in one transaction, publishes it to SNS and marks the rows published; failed rows stay pending.

`internal/pkg/uow` (Unit of Work) exists with a Postgres and an in-memory variant, but no production code path uses it today; the transactions above are opened directly. Do not add new code on top of it without deciding whether it stays.

## Dynamic SQL
`db.QueryBuilder(baseQuery)` (`internal/pkg/db/query_builder.go`) builds WHERE / GROUP BY / ORDER BY / LIMIT clauses. No production query uses it today; the current repositories use fixed SQL.

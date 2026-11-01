# Database

## PostgreSQL 15 Conventions
- Monetary columns: `NUMERIC(10,2)` — never `FLOAT` or `DOUBLE`.
- ServiceOrder status: `CHECK` constraint with 7 valid values.
- IDs: UUID for most entities; ULID for `service_order` and `service_order_history`.
- Timestamps: `TIMESTAMPTZ`, UTC, ISO 8601.
- Junction tables: `service_order_work` (SO ↔ Work), `service_order_supply` (SO ↔ Supply).

## Migration Strategy
All schema changes via versioned migration files only:
```
migrations/NNNNNN_description.up.sql
migrations/NNNNNN_description.down.sql
```
Never alter the schema manually. Never drop or modify existing columns — add new, deprecate old.

Commands:
```bash
make migrate-up      # apply pending
make migrate-down    # roll back last
make migrate-status  # current version
```

## Transaction Context Pattern
- `postgres.GetTransaction(ctx)` — gets `*sql.Tx` for mutating ops (called inside a UoW step).
- `postgres.GetOneTimeTransaction(ctx)` — gets a `*sql.DB` connection for read-only ops.

See `architecture.md` for the full UoW pattern.

## Required Indexes
`customer.cpf`, `customer.cnpj`, `vehicle.license_plate`, `service_order.status`, `service_order.customer_id`, `user.email`.

## Required Foreign Keys
`vehicle→customer`, `service_order→customer`, `service_order→vehicle`,
`service_order_work→(service_order, work)`, `service_order_supply→(service_order, supply)`,
`customer→user`.

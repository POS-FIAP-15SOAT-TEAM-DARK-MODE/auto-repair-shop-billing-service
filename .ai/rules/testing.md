# Testing

## Coverage Thresholds
- Critical packages (`internal/budget`, `internal/payment`): **90% minimum**.
- Total (`go test ./...`): **80% minimum** (enforced by the `coverage` CI job).

## Commands
```bash
make test        # all tests with race detector (LOG_LEVEL=PANIC suppresses noise)
make coverage    # generate coverage.out and open HTML report
go test ./internal/payment/... -run TestTransition -v
go test ./internal/budget/... ./internal/payment/... -v --race
```

## Unit Tests — In-Memory Implementations
- Repositories: `internal/<pkg>/repository/in_memory.go`; Postgres repositories are tested with `go-sqlmock`.
- Handlers in `internal/billing` are tested with in-memory repositories, a fake provider and sqlmock for the transaction and outbox.

Pattern:
```go
// Arrange
budgets  := repository.NewInMemory()
payments := repository.NewInMemory()
svc      := billing.NewService(db, budgets, payments, provider, newID)
// Act
err := svc.GenerateBudget(ctx, tx, env)
// Assert
assert.NoError(t, err)
```

## Integration Tests
- Use a real test PostgreSQL DB.
- Each test rolls back after completion — never leaves persistent state.
- Located in `internal/infra/repository/*/` and HTTP flow test files.

## Test Structure Rules
- Arrange / Act / Assert; tests must be fully independent.
- Naming: `Test<Unit>_<Condition>_<ExpectedOutcome>`.
- Group with sub-tests (`t.Run`).
- Never share mutable state between tests.

## What to Cover
| Domain | Critical cases |
|---|---|
| Payment transitions | Every pair of statuses (valid applied, invalid rejected, repeated rejected) |
| Budget | Works only, supplies only (rejected: no work), combined, rounding, negative price |
| CPF / CNPJ / plate | Valid, invalid format, invalid check digits, edge cases |
| Messaging | Duplicate `message_id` applied once, unknown type discarded, handler error keeps message |
| RBAC | 403 for out-of-scope roles, 401 for missing JWT |
| Contracts | Each message in `testdata/contracts/<Type>.json` decodes and encodes equal |

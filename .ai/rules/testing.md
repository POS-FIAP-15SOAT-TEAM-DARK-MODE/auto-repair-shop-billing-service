# Testing

## Coverage Thresholds (enforced by the `coverage` job in `.github/workflows/ci.yml`)
- `internal/budget/...` and `internal/payment/...`: **90% minimum**.
- Total (`go tool cover -func` on `go test ./... -coverprofile`): **80% minimum**.

Latest measured total: see the coverage section of `README.md`.

## Commands
```bash
make test              # all tests with the race detector (LOG_LEVEL=PANIC suppresses log noise)
make coverage          # writes coverage.out and prints the total
make test-integration  # -tags=integration: needs Postgres and MiniStack (see docker-compose.yml)
go test ./internal/payment/... -run TestTransition -v
go test ./internal/budget/... ./internal/payment/... -v --race
```

## Test Doubles
- No mocks are generated. Tests use hand-written fakes in `*_test.go` (for example `errBudgets` and `errPayments` in `internal/billing/service_test.go`).
- In-memory repositories: `internal/budget/repository/in_memory.go`, `internal/payment/repository/in_memory.go`.
- Postgres repositories and the transaction/outbox code are tested with `go-sqlmock`.
- Payment provider: use the real `payment.NewInternalProvider(mode)`; it has no side effects.

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
- `internal/integration/billing_test.go`, build tag `integration`. Uses a real Postgres and MiniStack (SQS/SNS). Env vars are listed at the top of the file.
- Each test must leave no persistent state.

## Contract Tests
`internal/billing/contract_test.go` loads each `testdata/contracts/<Type>.json` and checks that the envelope and payload decode and encode back equal. These files are copies of `docs/fase4/contracts/messaging.md` in the auto-repair-shop monorepo; change them together.

## Test Structure Rules
- Arrange / Act / Assert; tests must be fully independent.
- Naming: `Test<Unit>_<Condition>_<ExpectedOutcome>` is preferred for new tests (existing ones include `TestNewBudget_TotalOnlyWorks`, `TestGenerateBudget_AlreadyGeneratedIsDeduped`).
- Group related cases with sub-tests (`t.Run`).
- Never share mutable state between tests.

## What to Cover
| Area | Critical cases |
|---|---|
| Payment transitions | Every pair of statuses (valid applied, invalid rejected, repeated rejected) |
| Budget | Works only, supplies only (rejected: no work), combined, rounding, negative price, invalid quantity |
| Messaging | Duplicate `message_id` applied once, unknown type discarded, handler error keeps the message |
| REST and RBAC | 403 for out-of-scope roles, 401 for missing or invalid JWT, 404 for another customer's budget |
| Contracts | Each message in `testdata/contracts/<Type>.json` decodes and encodes equal |

# Workflow

## Conventional Commits

Format:
```
<type>(<scope>): <short description>

[optional body]

[optional footer(s)]
```

Types: `feat` · `fix` · `refactor` · `test` · `chore` · `docs` · `perf` · `style` · `revert`

Scopes: `service-order` · `customer` · `vehicle` · `work` · `supply` · `stock` · `budget` · `auth` · `user` · `infra` · `db` · `api` · `integration`

Rules:
- Subject max 72 chars, lowercase, no trailing period.
- Imperative mood: "add feature" not "added feature".
- Breaking changes: `feat(auth)!: ...` or `BREAKING CHANGE:` footer.
- Reference issues in footer: `Closes #123`.

## Pre-commit Hooks
Configured in `.pre-commit-config.yaml`:
- `trailing-whitespace`, `end-of-file-fixer`, `check-yaml`, `check-added-large-files`, `check-merge-conflict`
- `golangci-lint` (v2.9.0)
- `go fmt ./...`
- `go build`
- `go-unit-tests`
- `go vet ./...`
- `check-ai-rules`: enforces `.ai/` as the only source of truth for rule files

## Development Commands
```bash
make run          # start server on port 8080
make test         # all tests with race detector
make coverage     # HTML coverage report
make docker-up    # build + start app + PostgreSQL
make migrate-up   # apply pending migrations
make mockgen      # regenerate all mocks via go generate
```

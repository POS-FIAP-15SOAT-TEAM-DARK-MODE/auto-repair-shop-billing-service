# Workflow

## Conventional Commits

Format:
```
<type>(<scope>): <short description>

[optional body]

[optional footer(s)]
```

Types: `feat` · `fix` · `refactor` · `test` · `chore` · `docs` · `perf` · `style` · `revert`

Scopes used in this service: `billing` · `budget` · `payment` · `messaging` · `auth` · `db` · `api` · `web` · `middleware` · `routing` · `bootstrap` · `integration` · `k8s` · `ci` · `docs` · `deps` · `compose`

Rules:
- Subject max 72 chars, lowercase, no trailing period.
- Imperative mood: "add feature" not "added feature".
- Breaking changes: `feat(auth)!: ...` or `BREAKING CHANGE:` footer.
- Reference issues in footer: `Closes #123`.

## Branches
`feature → develop → main` (ADR 0001). `develop` deploys to STG and `main` to PRD through `.github/workflows/docker.yml`.

## Pre-commit Hooks
Configured in `.pre-commit-config.yaml`:
- `trailing-whitespace`, `end-of-file-fixer`, `check-yaml`, `check-added-large-files`, `check-merge-conflict`
- `golangci-lint` (v2.9.0)
- `go fmt ./...`
- `go build`
- `go-unit-tests`
- `go vet ./...`
- `check-ai-rules`: enforces `.ai/` as the only source of rule files (`.ai/hooks/check-ai-rules.sh`)

## Development Commands
```bash
make run               # go run cmd/service/main.go (needs env vars or .env)
make test              # all tests with race detector (LOG_LEVEL=PANIC)
make test-integration  # -tags=integration against Postgres and MiniStack
make coverage          # coverage.out + total
make up                # docker compose: app, Postgres, migrations, MiniStack, topic/queue setup
make down              # docker compose down -v
make migrate-up        # apply migrations with the golang-migrate CLI (see make migrate-install)
```

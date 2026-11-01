# CLAUDE.md

> **Single source of truth: `.ai/`**
> Only this file is auto-loaded by Claude Code. All linked files must be opened on demand — never loaded automatically.
> Never duplicate rule content here.

## Always-Applied Rules

### 1. Security
- Never log or expose CPF, CNPJ, passwords, or JWT tokens in any response, log entry, or error message.
- bcrypt min cost 12; auth middleware must not make extra DB calls — rely entirely on JWT payload.
- Validate all input at handler layer; never trust raw strings inside services or domain.

### 2. Git Conventions
All commits must follow Conventional Commits: `<type>(<scope>): <description>`.
Types: `feat fix refactor test chore docs perf style revert` — scopes in `.ai/rules/workflow.md`.

### 3. Layer Contract (verify after every edit)
- Handlers: zero business logic.
- Services: zero SQL.
- Domain models: zero framework imports (no Gin, no `lib/pq`).

---

## Task Routing

| Task type | Files to read |
|---|---|
| New feature or domain change | `.ai/rules/architecture.md` · `.ai/rules/domain.md` · `.ai/rules/go-conventions.md` |
| Database schema or migration | `.ai/rules/database.md` · `.ai/rules/go-conventions.md` |
| API endpoint | `.ai/rules/api.md` · `.ai/rules/architecture.md` · `.ai/rules/security.md` |
| Auth, RBAC, or security | `.ai/rules/security.md` · `.ai/rules/domain.md` |
| Tests | `.ai/rules/testing.md` · `.ai/rules/architecture.md` |
| Logging or observability | `.ai/rules/observability.md` · `.ai/rules/security.md` |
| Git workflow | `.ai/rules/workflow.md` |
| Scaffold new domain | `.ai/commands/new-domain.md` |
| Add migration | `.ai/commands/add-migration.md` |

All rule files: `.ai/rules/` · All commands: `.ai/commands/` · Index: `.ai/README.md`

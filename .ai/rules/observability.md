# Observability

## Logger
Structured JSON via Zap. Initialized in `internal/pkg/logger/logger.go` with centralized sensitive-field redaction (Issue #223).

## Log Levels
| Level | Use |
|---|---|
| `INFO` | Business events (service order created, status changed) |
| `WARN` | Business rule violations (invalid transition attempt, out-of-stock) |
| `ERROR` | Unexpected failures (DB errors, unhandled panics) |

## Canonical Event Names (Issue #236)
Reuse these — do not invent new names for the same operations:
- `service_order.created`
- `service_order.status_transition` — fields: `service_order_id`, `from_status`, `to_status`
- `service_order.validation_failed`
- `service_order.status_notification_sent` — customer status notification (e-mail stand-in); `recipient` is masked
- `service_order.status_notification_failed` — notifier delivery error (best-effort; never fails the transition)

## Sensitive Field Redaction
The logger automatically redacts keys matching: CPF, CNPJ, password, token, dsn, database_url.
- Prefer explicit fields (`zap.String("customer_id", id)`) over `zap.Any("customer", obj)`.
- Never add logging that passes raw domain objects or request bodies with PII.

## Read Operations
- Keep success logs minimal (debug or omit).
- Always log errors from DB calls at `ERROR` level with operation context.

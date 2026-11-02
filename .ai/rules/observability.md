# Observability — Billing Service

## Logs
Zap JSON, one line per event. Every line carries `service` (from `SERVICE_NAME`, default `billing-service`).
Request lines add `request_id` (from `X-Request-Id`, or generated). Consumer lines add `message_id`, `type`,
`request_id`, `saga_id` and `service_order_id` when the message has them (see `messaging.Envelope.logFields`).
Never log CPF/CNPJ, passwords, JWT or the `Authorization` header (`logger.isSensitiveKey`).

Event names follow `<entidade>.<ação>` in the `event` field. The message text is kept stable because existing
Loki queries match on it.

| `event` | Message text | Where |
|---|---|---|
| `request.started` / `request.completed` / `request.failed` | `starting request` / `ending request` / error text | Logger middleware |
| `request.panic` | `panic: ...` | Recovery middleware |
| `budget.already_generated` | `budget already generated, nothing to do` | GenerateBudget dedupe |
| `payment.request_discarded` (`reason`: `no_generated_budget`, `amount_differs`, `open_payment_exists`) | `request payment discarded: ...` | RequestPayment |
| `budget.generated`, `budget.cancelled`, `payment.approved`, `payment.failed`, `payment.refunded` | `event enqueued` | outbox write (same tx as the state change) |
| `message.published` | `message published` | outbox relay, after commit |
| `message.duplicate_ignored` | `duplicate message ignored` | consumer dedupe |
| `message.unknown_type_discarded` | `unknown message type, discarding` | consumer |
| `message.invalid` | decode error | consumer |

Component `outbox-relay` / `sqs-consumer` marks errors of the background loops (the message stays unpublished / in the queue).

## Metrics
Registered in `internal/pkg/metrics` and served at `/metrics`.
- `http_request_duration_seconds` (method, route, status)
- `outbox_pending` (gauge): outbox rows not yet published, refreshed by the relay on every tick.

Saga counters (`saga_started_total`, `saga_completed_total`, `saga_compensated_total`, `saga_step_duration_seconds`)
belong to the service-order service, which runs the saga. Billing only takes part in it, so it does not emit them.
`dlq_messages` is not implemented here: it needs the DLQ URL wired into the deploy workflow.

## Redaction
Prefer explicit fields (`zap.String("service_order_id", id)`). Avoid `zap.Any` on request or domain objects.

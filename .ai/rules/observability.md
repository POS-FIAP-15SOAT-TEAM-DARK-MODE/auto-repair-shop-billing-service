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
| `webhook.rejected` (`reason`: `invalid_signature`, `body_too_large`) | `payment webhook rejected: invalid signature` / `payment webhook rejected: body too large` | the Mercado Pago webhook refused the request (401 and 400) |
| `payment.status_applied` (`from`, `to`, `gateway_payment_id`) | `payment status applied from the gateway` | a Mercado Pago payment changed a payment status |
| `gateway.payment_not_applied` (`reason`: `status_not_mapped`, `invalid_transition`, `unknown_payment`, `not_found`) | `gateway status not mapped, ignoring` / `gateway status ignored: invalid transition` / `gateway payment of an unknown payment` / `gateway payment of a notification not found` | gateway payment not applied |
| `payment.unsettled` (`reason`: `extra_approval`, `late_approval`, `amount_mismatch`) | `extra approved gateway payment: refund scheduled` / `late approval on a closed payment: refund scheduled` (warn) / `approved gateway payment with another amount, not settled` (error: needs a person) | an approved gateway payment Billing did not settle; logged once per gateway payment, after the commit |
| `payment.unsettled_refunded` | `unsettled payment refunded` | the gateway confirmed the refund of an unsettled payment |
| `gateway.worker_step_failed` (`step`: `CREATE_CHECKOUT`, `RECONCILE`, `EXPIRE`, `REFUND`, `CLOSE_CHECKOUT`, `REFUND_UNSETTLED`; `transient`) | the error text | the gateway worker retries with a backoff. A `REFUND_UNSETTLED` or `REFUND` step that is refused for good (a payment too old to refund, a payment the gateway does not know) is retried forever every 5 minutes: it needs a person |
| `payment.refund_stuck` (warn; `age_seconds`) | `refund not confirmed by the gateway` | a requested refund still unconfirmed after 30 minutes; logged on each retry |
| `gateway.worker_waiting` (debug) | `waiting for the gateway` | a worker step that only waits for the gateway |
| `gateway.worker_failed` (`step`: a work kind, `LOCK`, `REFUND_UNSETTLED` or `PENDING_STATS`) | the error text | listing, stamping, the work lock (it could not be asked, or its session was lost mid-pass) or the pending stats of the worker failed |

Component `outbox-relay` / `sqs-consumer` marks errors of the background loops (the message stays unpublished / in the queue).

## Metrics
Registered in `internal/pkg/metrics` and served at `/metrics`.
- `http_request_duration_seconds` (method, route, status)
- `outbox_pending` (gauge): outbox rows not yet published, refreshed by the relay on every tick.
- `payment_status_changes_total{to,source}` — payments that changed status; `source` is `gateway` (webhook, reconciliation, refund), `worker` (expiry, refused checkout) or `manual` (attendant).
- `gateway_requests_total{op,outcome}` and `gateway_request_duration_seconds{op}` — calls to Mercado Pago (`create_checkout`, `find_payments`, `find_checkout`, `get_payment`, `refund`, `cancel_payment`, `expire_checkout`; outcome `ok`, `transient`, `permanent`). Watch `permanent` and a rising `transient` (a rejected token shows up here).
- `gateway_worker_passes_total{result}` — passes of the gateway worker: `ran`, `skipped` (another replica holds the work lock; normal on all but one replica), `error` (the lock could not be asked) or `lost` (the session that held the lock died mid-pass, so the pass stopped). Alert if no replica reports `ran`, or on `lost`.
- `gateway_worker_errors_total{step,kind}` — failed worker steps; waiting for the gateway is not an error.
- `payments_pending` and `payments_pending_oldest_age_seconds` — Mercado Pago payments waiting; refreshed every worker pass. An age above the checkout lifetime means expiry is stuck.
- `refunds_pending` and `refunds_pending_oldest_age_seconds` — refunds the saga asked for plus unsettled payments to give back, that Mercado Pago has not confirmed. **Alert when the age passes about 30 minutes**: the refund is stuck (refused, or never reported `refunded`).
- `payments_unsettled_total{reason}` — approved gateway payments Billing did not settle, counted once per gateway payment, however many times they are seen (`extra_approval` and `late_approval` are refunded by the worker; `amount_mismatch` is kept in `unsettled_payment` with status `REVIEW` for a person). **Alert on `amount_mismatch`**, and on the refund backlog below for the others.
- Labels never carry ids (no payment, order or gateway ids), so cardinality stays fixed.

Saga counters (`saga_started_total`, `saga_completed_total`, `saga_compensated_total`, `saga_step_duration_seconds`)
belong to the service-order service, which runs the saga. Billing only takes part in it, so it does not emit them.
`dlq_messages` is not implemented here: it needs the DLQ URL wired into the deploy workflow.

## Redaction
Prefer explicit fields (`zap.String("service_order_id", id)`). Avoid `zap.Any` on request or domain objects.

# Observability — Billing Service

Zap JSON logs with `request_id`, `saga_id`, `service_order_id`, `message_id` when available.

Log messages in use (keep them stable; dashboards and Loki queries depend on them):
- `budget already generated, nothing to do` — GenerateBudget deduped
- `request payment discarded: ...` — RequestPayment ignored (no generated budget, amount differs, open payment exists)
- `duplicate message ignored` — consumer found `message_id` in `processed_message`
- `unknown message type, discarding` — consumer discarded an unknown type
- `payment status applied from the gateway` — a Mercado Pago payment changed a payment status (fields `from`, `to`, `gateway_payment_id`)
- `gateway status not mapped, ignoring` / `gateway status ignored: invalid transition` / `gateway payment of an unknown payment` — gateway payment not applied
- errors `extra approved gateway payment`, `late approval on a closed payment`, `approved gateway payment with another amount` (all `needs a manual refund` or review) — money taken that Billing did not settle; alert on them
- component `gateway-worker` (field `step`: CREATE_CHECKOUT, RECONCILE, EXPIRE, REFUND, CLOSE_CHECKOUT, `transient`) — errors of the gateway worker loop; it retries with a backoff
- component `outbox-relay` / `sqs-consumer` — errors of the background loops (message stays unpublished / in the queue)

Redaction: never log customer documents, passwords or JWT tokens. Prefer explicit fields (`zap.String("service_order_id", id)`).

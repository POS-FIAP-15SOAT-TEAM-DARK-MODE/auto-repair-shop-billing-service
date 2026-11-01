# Observability — Billing Service

Zap JSON logs with `request_id`, `saga_id`, `service_order_id`, `message_id` when available.

Log messages in use (keep them stable; dashboards and Loki queries depend on them):
- `budget already generated, nothing to do` — GenerateBudget deduped
- `request payment discarded: ...` — RequestPayment ignored (no generated budget, amount differs, open payment exists)
- `duplicate message ignored` — consumer found `message_id` in `processed_message`
- `unknown message type, discarding` — consumer discarded an unknown type
- component `outbox-relay` / `sqs-consumer` — errors of the background loops (message stays unpublished / in the queue)

Redaction: never log customer documents, passwords or JWT tokens. Prefer explicit fields (`zap.String("service_order_id", id)`).

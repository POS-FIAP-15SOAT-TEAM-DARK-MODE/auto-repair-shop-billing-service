# Database — Billing Service (PostgreSQL database `billing`)

- Migrations: `migrations/` (golang-migrate), each with `.up.sql` and `.down.sql`. No seed data.
- IDs: `VARCHAR(36)` UUID; `service_order_id` is `VARCHAR(26)` (ULID).
- Money: `NUMERIC(10,2)`. Timestamps: `TIMESTAMP WITH TIME ZONE`.
- `payment` has the Mercado Pago columns (`external_id`, `gateway_payment_id`, `checkout_url`, `expires_at`, `refund_requested_at`, `checkout_closed_at`, `last_checked_at`; migrations `000002`, `000003`), NULL for the internal provider.
- The worker queries (`ListWork`) are served by partial indexes from migration `000005`, all ending in `last_checked_at NULLS FIRST, created_at, id` like their `ORDER BY`: `idx_payment_gateway_work` (create, reconcile, expire; by `status`), `idx_payment_refund_work` (approved with a refund requested) and `idx_payment_close_work` (finished and not closed). Keep them in step with `ListWork`.
- Tables: `budget`, `budget_item`, `payment`, `unsettled_payment` (approved gateway payments to refund, or kept for review when the amount differs; migration `000004`), `processed_message` (consumer dedupe), `outbox` (publisher).
- Partial unique indexes: `uq_budget_active_per_so` (one `GENERATED` budget per SO), `uq_payment_open_per_so` (one `PENDING`/`APPROVED` payment per SO).
- No foreign keys across databases. Foreign keys inside the database: `budget_item → budget`, `payment → budget`, `unsettled_payment → payment`.
- The application user (`billing_app`) only connects to `billing`.

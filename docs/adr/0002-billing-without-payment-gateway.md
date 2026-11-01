# ADR 0002 — Billing without an external payment gateway

- Status: Accepted
- Tags: billing, payment, saga, messaging

## Context

Billing issues budgets and charges for service orders, and participates in the saga orchestrated by
Service Order. A real gateway (Mercado Pago) is out of scope for this phase, but the saga must be
demonstrable in all outcomes: approved, rejected (compensation), and pending until an attendant decides.

## Decision

1. `payment.PaymentProvider` (`Charge`, `Refund`) is the seam for gateways. The only implementation is
   `InternalProvider`, selected by `PAYMENT_MODE`:
   - `auto-approve` → `APPROVED` (outcome `PaymentApproved`);
   - `auto-reject` → `REJECTED` (outcome `PaymentFailed`, reason `PAYMENT_REJECTED`, which triggers compensation);
   - `manual` (default) → stays `PENDING` until the attendant calls `POST /v1/payments/:id/confirm|reject`.
2. `Charge` runs **inside** the database transaction of the command. This is safe for the internal provider,
   which has no side effects.
3. `payment.saga_id` is stored from the `RequestPayment` envelope, so manual `confirm`/`reject` events
   carry the saga of the payment. `request_id` comes from the `X-Request-Id` header of the REST call.
4. Monetary values are `shopspring/decimal` with 2 places and travel as decimal strings.
5. Duplicated `RequestPayment` messages for an order that already has an open payment are discarded
   (logged, no error), protected by the partial unique index `uq_payment_open_per_so`.

## Consequences

- A gateway such as Mercado Pago is added as a new `PaymentProvider` implementation. Because a remote call
  must not hold a database transaction, that provider requires splitting the flow: persist `PENDING`,
  commit, call `Charge`, then record the outcome in a second transaction, with reconciliation for
  outcomes lost between the two steps. This is not needed for the internal provider.
- `Refund` for the internal provider is a no-op; the state transition and the `PaymentRefunded` event are still recorded.
- The saga remains valid for every mode without changes in Service Order.

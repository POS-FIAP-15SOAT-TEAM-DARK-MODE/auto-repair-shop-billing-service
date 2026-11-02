# ADR 0003 — Mercado Pago provider (Checkout Pro)

- Status: Accepted
- Tags: billing, payment, saga, mercadopago
- Supersedes: the "without a payment gateway" part of ADR 0002. The internal modes it describes stay.
- Background: [RFC 0001](../rfc/0001-mercadopago-checkout-integration.md) (options, sandbox results and the open items)

## Context

The challenge requires the payment part of Billing to integrate with Mercado Pago. ADR 0002 left a
`PaymentProvider` seam and said a remote provider needs the flow split in two, because a remote call must
not hold the database transaction of a command. The saga, its message contracts and the `manual`,
`auto-approve` and `auto-reject` modes must not change (project decision D11).

## Decision

1. **New mode.** `PAYMENT_MODE=mercadopago` charges through Mercado Pago Checkout Pro. The other modes
   are untouched, and so are the commands, the events and `PaymentFailed.reason` (`PAYMENT_REJECTED`).
2. **Gateway port.** `payment.Gateway` (create/find checkout, find/get payments, refund, cancel payment,
   expire checkout) lives in `internal/payment`; `internal/payment/mercadopago` is the only package that
   imports the official SDK. Failures are classified as transient or permanent (see "Resilience").
3. **`RequestPayment` only records.** In this mode it creates a `PENDING` payment with `expires_at` (12 h by
   default) and returns. Nothing remote happens inside the command transaction.
4. **A gateway worker does the remote work**, one pass every few seconds. Each step reads its work from the
   database (`WorkKind`), calls the gateway outside a transaction and records the result in a short one:
   open the checkout, reconcile pending payments by `external_reference`, expire unpaid checkouts, request
   refunds, close the checkout of finished payments, and refund the unsettled payments (decision 6).
5. **One place changes a status.** `ApplyGatewayPayment` is used by the webhook, the reconciliation and the
   refund confirmation: it locks the row (`FOR UPDATE`), validates the transition and enqueues the saga event
   in the same transaction. It settles against `transaction_amount` (installment interest is not part of the
   price).
6. **Unsettled payments.** An approved gateway payment that cannot settle the payment is never applied:
   - a **late** approval (the payment is `CANCELLED`, `REJECTED` or `REFUNDED`) or an **extra** one (the
     payment was already settled by another) is recorded in `unsettled_payment` (migration `000004`) in the
     same transaction and **refunded by the worker**. The saga is not told: it has moved on.
   - an approval for **another amount** is recorded with status `REVIEW` and is never refunded
     automatically: a person decides.
   Each gateway payment is recorded, logged and counted once, after the commit, however many times it is seen.
7. **Webhook.** `POST /v1/webhooks/mercadopago` is public and registered only in this mode. The signature is
   verified first (SDK helper, 5 min tolerance); the body is never trusted for payment data, the payment is
   read from Mercado Pago by `data.id`; other types are acknowledged; a body over 64 KB is refused (400); a
   notification of a payment Mercado Pago does not know is acknowledged (200 `unknown`); other failures
   answer 5xx so it retries.
8. **Rejection and expiry are final.** A rejected payment expires its checkout and replies `PaymentFailed`.
   When the 12 h pass, an unpaid checkout becomes `CANCELLED` and an `in_process` payment is cancelled at
   the gateway and becomes `REJECTED`; both reply `PaymentFailed`. An approval found first wins. The
   orchestrator's `SAGA_PAYMENT_TIMEOUT` is 24 h, so the checkout lives 12 h: Billing answers first and the
   timeout stays a backstop. With a longer checkout the orchestrator would cancel the order (`SAGA_TIMEOUT`)
   and send `RefundPayment` before Billing replies.
9. **Refund.** `RefundPayment` on an approved gateway payment only records `refund_requested_at`;
   `PaymentRefunded` is published after Mercado Pago confirms. On a pending payment it cancels it, and the
   worker expires the checkout and cancels an in-process gateway payment.
10. **Manual endpoints.** `confirm` and `reject` answer `409` for every payment charged through Mercado Pago,
    with or without a checkout yet, so an attendant's decision cannot race the checkout creation.
11. **Visibility.** `GET /v1/payments/:serviceOrderId` stays attendant and admin only (project decision R10)
    and returns `checkout_url` and `expires_at`; the attendant hands the link to the customer.
12. **Replicas take turns.** Each worker pass starts with a Postgres advisory lock (`pg_try_advisory_lock`,
    no waiting); the replica that gets it runs the pass and the others skip it. Without it two replicas would
    open the checkout of the same payment and leave a second payable preference. The lock belongs to a
    session, so it never holds a transaction open during a remote call, it is released if the replica dies,
    and it needs a connection in session mode (not a transaction-mode pooler). `FOR UPDATE SKIP LOCKED` was
    not used: it would keep the row locked during the call.
13. **Data.** Migration `000002` adds the gateway columns to `payment` (`external_id`, `gateway_payment_id`,
    `checkout_url`, `expires_at`, `refund_requested_at`, `checkout_closed_at`); `000003` adds
    `last_checked_at`; `000004` creates `unsettled_payment`; `000005` adds the partial indexes of the worker
    queries (an index-only scan, 0.03 ms with 60,000 payments). `000001` is not edited.

## Consequences

### Resilience
- **Backoff.** A failing step backs off (5 s doubling up to 5 min) and never blocks other payments. The backoff
  and the "refund already submitted" marker live in memory: after a restart a step simply runs again, which is
  safe because the refund, the cancellation and the reads are idempotent.
- **Error classes.** 401, 403, 408, 409, 429 and 5xx are retried (a rotated token must not fail customers'
  payments); only 400, 404 and 422 are permanent. A 404 on a notification makes it "unknown" (200).
- **Fairness.** The worker stamps `last_checked_at` and serves the least recently checked first, so a payment
  that keeps failing cannot starve the others; the reconciliation looks at a payment at most every 30 s.
- **A checkout whose creation answer was lost.** Mercado Pago ignores the idempotency key when it creates a
  preference (the sandbox smoke test got two for one key), so a retried creation first searches an open
  preference of the same `external_reference` (the search sees a new one after about 3 s). The reconciliation
  and the closing cover payments with or without a stored `external_id`; closing one without it asks the gateway
  for the checkout (`FindCheckout`) and expires what it finds. The creation step does not stamp
  `last_checked_at`, so a payment whose creation keeps failing is still reconciled.
- **A refund reads the payment first.** The sandbox answers `401` to every refund request, which counts as
  transient. Reading before asking lets a refund made from the seller's panel settle the payment (or close the
  unsettled record) without a webhook, and lets the stuck-refund warning fire even while the request is refused.
  This was found in the live test of decision 6.
- **An approval that cannot be settled** keeps the payment `PENDING` for a person: the expiry does not cancel a
  payment the customer was charged for, while another matching approval is still applied.
- **Bounded passes.** A pass has a timeout and checks the lock is still held before each step; if the session
  was lost, the pass stops (`gateway_worker_passes_total{result="lost"}`).

### Money and contracts
- **Money as a float, in one place.** Money is `decimal` everywhere. The only exception is the edge with the
  SDK, which models `unit_price` and `transaction_amount` as `float64`: the amount is rounded to two places and
  converted when the checkout is created, and read back with `NewFromFloat(...).Round(2)`. A test checks that
  amounts from 0.01 to 99,999,999.99 (the `NUMERIC(10,2)` range) survive the round trip without losing a cent.
- **`charged_back` is `REFUNDED`** and publishes `PaymentRefunded`, which the orchestrator accepts and only
  logs (`saga.reply_logged`): a chargeback never moves a service order.
- **`/metrics` is not exposed**: the gateway routes only `/v1/*` and the lambda login, and blocks `/internal/*`.
- The internal modes, the contracts and the gateway's end-to-end suite keep working unchanged. Row locks make
  the webhook, the worker and the manual endpoints safe against each other.

### Known gaps (listed in the RFC)
- The refund through the Mercado Pago API returned `401 code 7` ("Unauthorized use of live credentials") in
  the sandbox, in the POC and in the live test of this service. Refunds, including the ones of unsettled
  payments, are retried with a backoff until the gateway reports them refunded; alert on
  `refunds_pending_oldest_age_seconds`. A refused-for-good refund (a payment too old to refund, a payment the
  gateway does not know) is retried every 5 minutes and needs a person.
- A refund or chargeback made outside the saga on an `APPROVED` payment is seen only through the webhook, because
  the reconciliation covers `PENDING` payments.
- The real webhook is verified once the service runs in the AWS Lab (it needs a public HTTPS URL).

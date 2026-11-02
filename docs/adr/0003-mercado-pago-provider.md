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
2. **Gateway port.** `payment.Gateway` (create checkout, find/get payments, refund, cancel payment, expire
   checkout) lives in `internal/payment`; `internal/payment/mercadopago` is the only package that imports the
   official SDK. Failures are classified as transient (network, 429, 5xx) or permanent.
3. **`RequestPayment` only records.** In this mode it creates a `PENDING` payment with `expires_at` (12 h by
   default) and returns. Nothing remote happens inside the command transaction.
4. **A gateway worker does the remote work**, one pass every few seconds, each step reading its work from the
   database (`WorkKind`), calling the gateway outside a transaction and recording the result in a short one:
   open the checkout (the key `checkout-<payment id>` is sent, but Mercado Pago does not honor it for preferences, so a retry first searches an open preference of the payment), reconcile pending payments by
   `external_reference`, expire unpaid checkouts, submit refunds, and close the checkout of finished
   payments. Failures back off (5 s doubling up to 5 min) and never block other payments.
5. **One place changes a status.** `ApplyGatewayPayment` is used by the webhook, the reconciliation and the
   refund confirmation: it locks the row (`FOR UPDATE`), validates the transition and enqueues the saga event in
   the same transaction. It settles against `transaction_amount` (installment interest is not part of the
   price). It does not apply a late approval on a closed payment or a second approved payment: they are
   recorded in `unsettled_payment` (migration `000004`) in the same transaction and refunded by the worker (the
   saga is not told, it has moved on). An approval with another amount is only logged: it needs a person.
6. **Webhook.** `POST /v1/webhooks/mercadopago` is public and registered only in this mode. The signature is
   verified first (SDK helper, 5 min tolerance); the body is never trusted for payment data, the payment is
   read from Mercado Pago by `data.id`; other types are acknowledged; failures answer 5xx so it retries.
7. **Rejection and expiry are final.** A rejected payment expires its checkout and replies `PaymentFailed`.
   When the 12 h pass, an unpaid checkout becomes `CANCELLED` and an `in_process` payment is cancelled at the
   gateway and becomes `REJECTED`; both reply `PaymentFailed`. An approval found first wins. The orchestrator's
   `SAGA_PAYMENT_TIMEOUT` is 24 h today, so the checkout lives 12 h by default: Billing answers first and the
   timeout stays a backstop. With a checkout longer than the timeout, the orchestrator would cancel the order
   (`SAGA_TIMEOUT`) and send `RefundPayment` before Billing replies.
8. **Refund.** `RefundPayment` on an approved gateway payment only records `refund_requested_at`;
   `PaymentRefunded` is published after Mercado Pago confirms the refund. On a pending payment it cancels it,
   and the worker expires the checkout and cancels an in-process gateway payment.
9. **Manual endpoints.** `confirm` and `reject` answer `409` for every payment charged through Mercado Pago, with or without a checkout yet, so an attendant's decision cannot race the checkout creation.
10. **Visibility.** `GET /v1/payments/:serviceOrderId` stays attendant and admin only (project decision R10)
    and now returns `checkout_url` and `expires_at`; the attendant hands the link to the customer.
11. **Data.** Migration `000002` adds `external_id`, `gateway_payment_id`, `checkout_url`, `expires_at`,
    `refund_requested_at` and `checkout_closed_at` to `payment`. `000001` is not edited.

## Consequences

- The internal modes, the contracts and the gateway's end-to-end suite keep working unchanged.
- Every gateway call is outside a transaction and idempotent, so a crash or a retry cannot create a second
  checkout or a second refund; the reconciliation also settles payments without any webhook (the AWS Lab has
  no HTTPS URL for one).
- Row locks make the webhook, the worker and the manual endpoints safe against each other; the same locking
  is now used by `RefundPayment` and by `confirm`/`reject`.
- Known gaps, listed in the RFC: a refund through the Mercado Pago API returned `401` ("Unauthorized use of live
  credentials") in the sandbox, so a refund can stay requested and is retried with a backoff; the refund
  of a late or extra approval depends on the same API, so until the 401 is understood it stays recorded and
  retried (alert on `refunds_pending_oldest_age_seconds`); a refund or chargeback made outside the saga on an
  `APPROVED` payment is seen only through the webhook, because the reconciliation covers `PENDING` payments; the real webhook is verified once the service runs in
  the AWS Lab.
- The worker stamps `payment.last_checked_at` (migration `000003`) and serves the least recently checked first,
  so a payment that keeps failing cannot starve the others; the reconciliation looks at a payment at most every
  30 seconds. A gateway answer of 401, 403, 408 or 409 is retried (a rotated token must not fail customers'
  payments); only 400, 404 and 422 are permanent. A refused refund reads the payment before giving up, a
  payment that closes without knowing its gateway payment id is searched once, an approval that cannot be
  settled makes the worker back off, and a live attempt is not overridden by an older refusal. A payment in backoff is stamped too, so it
  goes to the back of the line. An approval that cannot be settled keeps the payment `PENDING` for a person (the
  expiry does not cancel a payment the customer was charged for), while another matching approval is still
  applied. A checkout that never opened expires locally without asking the gateway. Only a 404 makes a
  notification "unknown" (200); other refusals make Mercado Pago retry.
- A checkout whose creation went through but whose answer was lost (a crash or a timeout before its id was
  stored) is not forgotten: the reconciliation looks for payments by external reference whether or not a
  checkout is stored, a retried creation first reuses the open preference, and closing a payment that has no
  stored checkout asks the gateway for it (`FindCheckout`) and expires what it finds. The creation step does
  not stamp `last_checked_at`, so a payment whose creation keeps failing is still reconciled.
- **Money as a float, in one place.** Money is `decimal` everywhere (rule: never `float`). The only exception is
  the edge with the SDK, which models `unit_price` and `transaction_amount` as `float64`: the amount is rounded
  to two places and converted when the checkout is created (`mercadopago.CreateCheckout`), and converted back
  with `NewFromFloat(...).Round(2)` when a payment is read. A test checks that amounts from 0.01 to
  99,999,999.99 (the `NUMERIC(10,2)` range) survive the round trip without losing a cent.
- **`charged_back` is `REFUNDED`.** It publishes `PaymentRefunded`, which the orchestrator accepts and only logs
  (`saga.reply_logged`), so a chargeback never moves a service order. The money is gone either way; the saga is
  not asked to react.
- **`/metrics` is not exposed.** The gateway routes only `/v1/*` and the lambda login (`config/routes.yaml`),
  and blocks `/internal/*`; `/metrics`, `/ping` and `/swagger` of the services stay inside the cluster.
- A webhook body over 64 KB is refused (400 and a `webhook.rejected` log), not cut short: a notification is a
  few hundred bytes, and the payment is always read from Mercado Pago anyway.
- The worker queries are served by `idx_payment_gateway_work (status, last_checked_at NULLS FIRST, created_at,
  id)` (migration `000005`): an index-only scan, 0.08 ms with 40,000 payments.
- A refund reads the payment at the gateway before it asks for one. The sandbox answers `401` to every refund
  request, which is classified as transient and retried; reading first lets a refund made from the seller's
  panel settle the payment (or close the unsettled record) without a webhook, and makes the stuck-refund warning
  fire even while the request is refused. The live test of the unsettled-payment refund found this.
- The replicas take turns: each worker pass starts with a Postgres advisory lock (`pg_try_advisory_lock`, no
  waiting). The replica that gets it runs the pass and the others skip it (`gateway_worker_passes_total`).
  Without it every replica would list the same payments, and two replicas opening the checkout of one
  payment would leave a second payable preference, because Mercado Pago ignores the idempotency key when
  it creates one. The lock belongs to the session, so it never holds a transaction open during a remote
  call, and it is released if the replica dies. `FOR UPDATE SKIP LOCKED` was not used: it would keep the row
  locked during the call.
- The backoff and the "refund already submitted" marker live in memory. After a restart a step simply runs
  again, which is safe because the gateway calls are idempotent.

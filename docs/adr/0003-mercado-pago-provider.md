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
3. **`RequestPayment` only records.** In this mode it creates a `PENDING` payment with `expires_at` (48 h by
   default) and returns. Nothing remote happens inside the command transaction.
4. **A gateway worker does the remote work**, one pass every few seconds, each step reading its work from the
   database (`WorkKind`), calling the gateway outside a transaction and recording the result in a short one:
   open the checkout (idempotency key `checkout-<payment id>`), reconcile pending payments by
   `external_reference`, expire unpaid checkouts, submit refunds, and close the checkout of finished
   payments. Failures back off (5 s doubling up to 5 min) and never block other payments.
5. **One place changes a status.** `ApplyGatewayPayment` is used by the webhook, the reconciliation and the
   refund confirmation: it locks the row (`FOR UPDATE`), validates the transition and enqueues the saga event in
   the same transaction. It settles against `transaction_amount` (installment interest is not part of the
   price), and logs, without applying, a late approval on a closed payment, a second approved payment and an
   approval with another amount.
6. **Webhook.** `POST /v1/webhooks/mercadopago` is public and registered only in this mode. The signature is
   verified first (SDK helper, 5 min tolerance); the body is never trusted for payment data, the payment is
   read from Mercado Pago by `data.id`; other types are acknowledged; failures answer 5xx so it retries.
7. **Rejection and expiry are final.** A rejected payment expires its checkout and replies `PaymentFailed`.
   When the 48 h pass, an unpaid checkout becomes `CANCELLED` and an `in_process` payment is cancelled at the
   gateway and becomes `REJECTED`; both reply `PaymentFailed`. An approval found first wins. The orchestrator's
   `PaymentTimeout` is only a backstop and is configured longer than the checkout expiry.
8. **Refund.** `RefundPayment` on an approved gateway payment only records `refund_requested_at`;
   `PaymentRefunded` is published after Mercado Pago confirms the refund. On a pending payment it cancels it,
   and the worker expires the checkout and cancels an in-process gateway payment.
9. **Manual endpoints.** `confirm` and `reject` answer `409` for a payment that already has a checkout.
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
  credentials") in the sandbox, so a refund can stay requested and is retried with a backoff; a late or extra
  approval is only logged and needs a manual refund; the real webhook is verified once the service runs in
  the AWS Lab.
- The backoff and the "refund already submitted" marker live in memory. After a restart a step simply runs
  again, which is safe because the gateway calls are idempotent.

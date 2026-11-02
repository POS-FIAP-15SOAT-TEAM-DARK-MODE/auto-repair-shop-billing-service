# Payment gateway (Mercado Pago)

Design: `docs/adr/0003-mercado-pago-provider.md`. Background and sandbox results: `docs/rfc/0001-*.md`.

## Rules
- The SDK is imported only in `internal/payment/mercadopago`. The domain, the service and the handlers use `payment.Gateway` and its own types.
- Never call the gateway inside a database transaction. Read the work, call the gateway, then record the result in a short transaction.
- Every status change from the gateway goes through `Service.ApplyGatewayPayment` (row lock, guarded transition, saga event in the same transaction). Do not set a gateway payment status anywhere else.
- Only one replica runs a worker pass at a time: `WorkLock` (a Postgres advisory lock, `internal/pkg/db/postgres/advisory_lock.go`) is taken without waiting at the start of `ProcessGatewayWork`. It is a session lock, not a transaction, so it does not break the rule above. Do not remove it: two replicas opening the checkout of one payment leave a second payable preference.
- Reconciliation and closing work on every payment, with or without a stored `external_id`: the gateway may hold a checkout whose creation answer was lost. Closing a payment with no `external_id` asks the gateway for its checkout (`FindCheckout`) before marking it closed.
- Keep calls idempotent: refund `refund-<gateway payment id>`. Mercado Pago ignores the key when it creates a preference, so `CreateCheckout` first searches an open preference with the same `external_reference` (the search sees a new one after about 3 s) and the key `checkout-<payment id>` is only a precaution.
- A rejected token must be retried, never fail a payment: 401, 403 and the search's 400 `invalid_token` are transient.
- Settle against `transaction_amount` (the price). Installment interest is the buyer's and is not part of it.
- The contract has one failure reason, `PAYMENT_REJECTED`. A new message or reason needs the contract and the fixtures in `testdata/contracts/` changed in every service.
- The internal modes (`manual`, `auto-approve`, `auto-reject`) and their tests must keep passing unchanged.
- The webhook is public: verify the signature first, never trust the body for payment data, answer 5xx so the gateway retries.
- Never log the access token, the webhook secret, the notification body or the payer data. Tests and CI never call Mercado Pago (fake SDK requester).

## Sandbox facts
- Use `init_point`, not `sandbox_init_point`.
- Both buyer and seller must be test users of the same country.
- Cardholder names change the outcome: `APRO` approved, `OTHE` rejected, `CONT` in process.

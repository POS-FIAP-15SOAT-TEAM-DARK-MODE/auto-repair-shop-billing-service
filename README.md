# Auto Repair Shop — Billing Service

Owns **budgets** (quotes of a service order) and **payments** (charges of the budget total). It takes part
in the service-order saga: it consumes commands from `saga-commands` and publishes events to `billing-events`.
Port `8083`, PostgreSQL database `billing`.

## Saga messages
| Consumes (`saga-commands`, `target=billing`) | Effect | Publishes (`billing-events`) |
|---|---|---|
| `GenerateBudget` | creates the budget (dedupe if one is already `GENERATED`) | `BudgetGenerated` |
| `CancelBudget` | `GENERATED` → `CANCELLED` | `BudgetCancelled` |
| `RequestPayment` | creates a `PENDING` payment and charges it (in `mercadopago` mode it only records it; the worker opens the checkout) | `PaymentApproved` / `PaymentFailed` (none when pending) |
| `RefundPayment` | `PENDING` → `CANCELLED`, `APPROVED` → `REFUNDED` (in `mercadopago` mode the refund is requested and `PaymentRefunded` is sent once Mercado Pago confirms it) | `PaymentRefunded` (only for `APPROVED → REFUNDED`) |

Envelope and payloads: `testdata/contracts/*.json` (copied from `docs/fase4/contracts/messaging.md`).
Invalid payloads return an error, so the message goes to the DLQ after 5 receives.

## Payment modes (`PAYMENT_MODE`)
| Mode | Outcome of `RequestPayment` |
|---|---|
| `manual` (default) | stays `PENDING`; the attendant calls `confirm` or `reject` |
| `auto-approve` | `APPROVED` → `PaymentApproved` |
| `auto-reject` | `REJECTED` → `PaymentFailed` (drives the saga compensation) |
| `mercadopago` | `PENDING` with a Mercado Pago checkout; the payment settles when Mercado Pago approves, rejects or the checkout expires |

### Mercado Pago mode
`PAYMENT_MODE=mercadopago` charges through Checkout Pro. The attendant reads `checkout_url` from
`GET /v1/payments/:serviceOrderId` and gives the link to the customer. Design: ADR 0003 (options and sandbox
results: RFC 0001). The service refuses to start without the access token.

How a payment goes:
1. `RequestPayment` only records a `PENDING` payment with an expiry (`expires_at`). No remote call happens in the command.
2. The **gateway worker** opens the checkout, reconciles pending payments with Mercado Pago, expires unpaid checkouts, requests refunds and closes the checkout of finished payments. Every remote call is outside a database transaction.
3. The payment settles when Mercado Pago approves, rejects or the checkout expires, through the **webhook** (signed, never trusted for payment data) or the reconciliation, which also works without a public URL.
4. A refund asked by the saga is requested at Mercado Pago, and `PaymentRefunded` is sent once the gateway confirms it. A refund made from the seller's panel is noticed too.
5. A customer who pays a link whose payment was already cancelled, rejected or refunded, or who pays twice, is recorded in `unsettled_payment` and **refunded by the worker**; the saga is not told. An approval for another amount is recorded with status `REVIEW` and needs a person: it is never refunded automatically.

With several replicas only one runs the worker at a time (a Postgres advisory lock, which needs a connection in session mode). Migrations `000002` to `000005` add the gateway columns, `last_checked_at`, `unsettled_payment` and the worker indexes.

| Variable | Meaning |
|---|---|
| `MERCADOPAGO_ACCESS_TOKEN` | required; the long `APP_USR-...` token of the seller (test) account |
| `MERCADOPAGO_WEBHOOK_SECRET` | signs the webhook; empty rejects every webhook (payments then settle through the worker) |
| `MERCADOPAGO_NOTIFICATION_URL` | public HTTPS URL of `POST /v1/webhooks/mercadopago`; empty means "do not send" |
| `MERCADOPAGO_CHECKOUT_TTL` | lifetime of a checkout, default `12h` (keep it below the orchestrator's `SAGA_PAYMENT_TIMEOUT`, `24h`) |
| `MERCADOPAGO_WEBHOOK_TOLERANCE` | accepted distance between the signature timestamp and now, default `5m` |
| `GATEWAY_WORKER_INTERVAL` | how often the gateway worker runs, default `5s` |

What to watch (`/metrics`, details in `.ai/rules/observability.md`): `refunds_pending_oldest_age_seconds` (a refund the gateway never confirmed; alert past about 30 minutes), `payments_unsettled_total{reason="amount_mismatch"}` (needs a person), `gateway_worker_passes_total{result="ran"}` (alert if no replica runs) and `gateway_requests_total{outcome!="ok"}`.

Lab deploy (optional): the `Docker` workflow switches the lab to this mode when `MERCADOPAGO_ACCESS_TOKEN` (and `MERCADOPAGO_WEBHOOK_SECRET`) exist as GitHub secrets visible to this repository, preferably organization secrets restricted to it. The deploy adds them to `billing-service-secrets` and sets `PAYMENT_MODE=mercadopago` in the ConfigMap; without them the lab overlay keeps `auto-approve`. `stg` and `prd` keep reading their credentials from Secrets Manager. A deploy of the same image does not restart the pods: restart the deployment after changing the secrets. When the `gateway` layer of `infra-k8s` exists, the deploy also sets `MERCADOPAGO_NOTIFICATION_URL` to `<api endpoint>/<env>/v1/webhooks/mercadopago`; before that, payments settle through the reconciliation, not the webhook. Register the same URL and the signing secret in the Mercado Pago panel (Webhooks, test mode, event `Pagamentos`); the panel's "simulate notification" works too. Mercado Pago sends the `ts` of `x-signature` in seconds; the check accepts seconds and milliseconds.

Known limit: the refund through the Mercado Pago API answers `401` ("Unauthorized use of live credentials") in the sandbox, so refunds are retried with a backoff until the gateway reports them refunded (for example after a refund from the seller's panel). Tests use a fake SDK transport and never call Mercado Pago; `make test-sandbox` is the opt-in exception.

## REST API
| Route | Roles |
|---|---|
| `GET /v1/budgets/:serviceOrderId` | Attendant, Mechanic, Admin, Customer (owner only) |
| `GET /v1/payments/:serviceOrderId` | Attendant, Admin |
| `POST /v1/payments/:id/confirm` | Attendant, Admin (204, 409, 404) |
| `POST /v1/payments/:id/reject` | Attendant, Admin (204, 409, 404) |
| `POST /v1/webhooks/mercadopago` | public, only in `mercadopago` mode; authenticated by the `x-signature` header |

Swagger: `docs/swagger.yaml`, served at `/swagger/index.html` and `/swagger.yaml`.

## Layout
- `internal/budget`, `internal/payment` — domain rules and repositories (`repository/`: Postgres and in-memory).
- `internal/billing` — command handlers, REST controller and service wiring.
- `internal/pkg/messaging` — outbox, relay (SNS), consumer (SQS with dedupe). Reusable by other services.

## Running
```bash
cp .env.example .env
make up            # docker compose: app, Postgres, migrations, MiniStack and topic/queue setup
curl -fsS http://localhost:8083/ping
make down
```
Without Docker: `make test`, `make test-integration` (needs Postgres and MiniStack; see CI), `make coverage`. `make test-sandbox` is an opt-in smoke test of the Mercado Pago gateway against the real sandbox (needs `MERCADOPAGO_ACCESS_TOKEN`; the CI does not run it).

## Tests and coverage
`make test` runs everything with the race detector; `make test-integration` adds the `integration` tag (Postgres and MiniStack).
No mocks are generated: tests use hand-written fakes and the in-memory repositories.

Measured with `go test ./... -coverprofile` (statement coverage, no generated code to exclude):

| Package | Coverage |
|---|---|
| **Total** | **92.7%** |
| `internal/budget`, `internal/budget/repository` | 100.0% |
| `internal/payment`, `internal/payment/repository` | 100.0% |
| `internal/payment/mercadopago` | 99.2% |
| `internal/billing` | 94.3% |
| `internal/pkg/messaging` | 97.5% |
| `internal/pkg/db/postgres` | 97.3% |
| `internal/app/routing` | 90.9% |

CI enforces 80% total and 90% for `internal/budget` and `internal/payment` (`coverage` job in `.github/workflows/ci.yml`).
`cmd/service` has no tests and is included in the total at 0%.

## Messaging resources (local, prefix `auto-repair-shop-local-`)
`saga-commands` (topic) → `billing-queue` (filter `target=billing`, DLQ after 5 receives); `billing-events` (topic).
Created by `local/init-aws.sh`.

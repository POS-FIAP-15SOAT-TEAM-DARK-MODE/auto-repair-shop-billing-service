# Auto Repair Shop — Billing Service

Owns **budgets** (quotes of a service order) and **payments** (charges of the budget total). It takes part
in the service-order saga: it consumes commands from `saga-commands` and publishes events to `billing-events`.
Port `8083`, PostgreSQL database `billing`.

## Saga messages
| Consumes (`saga-commands`, `target=billing`) | Effect | Publishes (`billing-events`) |
|---|---|---|
| `GenerateBudget` | creates the budget (dedupe if one is already `GENERATED`) | `BudgetGenerated` |
| `CancelBudget` | `GENERATED` → `CANCELLED` | `BudgetCancelled` |
| `RequestPayment` | creates a `PENDING` payment and charges it | `PaymentApproved` / `PaymentFailed` (none when pending) |
| `RefundPayment` | `PENDING` → `CANCELLED`, `APPROVED` → `REFUNDED` | `PaymentRefunded` (only for `APPROVED → REFUNDED`) |

Envelope and payloads: `testdata/contracts/*.json` (copied from `docs/fase4/contracts/messaging.md`).
Invalid payloads return an error, so the message goes to the DLQ after 5 receives.

## Payment modes (`PAYMENT_MODE`)
| Mode | Outcome of `RequestPayment` |
|---|---|
| `manual` (default) | stays `PENDING`; the attendant calls `confirm` or `reject` |
| `auto-approve` | `APPROVED` → `PaymentApproved` |
| `auto-reject` | `REJECTED` → `PaymentFailed` (drives the saga compensation) |

Adding Mercado Pago later means a new implementation of `payment.PaymentProvider`; see ADR 0002 for the
transaction split it requires.

## REST API
| Route | Roles |
|---|---|
| `GET /v1/budgets/:serviceOrderId` | Attendant, Mechanic, Admin, Customer (owner only) |
| `GET /v1/payments/:serviceOrderId` | Attendant, Admin |
| `POST /v1/payments/:id/confirm` | Attendant, Admin (204, 409, 404) |
| `POST /v1/payments/:id/reject` | Attendant, Admin (204, 409, 404) |

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
Without Docker: `make test`, `make test-integration` (needs Postgres and MiniStack; see CI), `make coverage`.

## Messaging resources (local, prefix `auto-repair-shop-local-`)
`saga-commands` (topic) → `billing-queue` (filter `target=billing`, DLQ after 5 receives); `billing-events` (topic).
Created by `local/init-aws.sh`.

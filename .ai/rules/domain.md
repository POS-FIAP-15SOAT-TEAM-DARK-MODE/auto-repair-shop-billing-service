# Domain — Billing Service

Owns budgets and payments of service orders. Participates in the saga orchestrated by Service Order
(commands in `saga-commands`, events in `billing-events`). Contract: `docs/fase4/contracts/messaging.md`.

## Entities
| Entity | Description |
|---|---|
| `Budget` | Quote of a service order: `GENERATED` or `CANCELLED`. Total = Σ work.unit_price + Σ supply.unit_price × quantity |
| `BudgetItem` | Work (quantity 1) or supply (quantity > 0) line of a budget |
| `Payment` | Charge of a budget total: `PENDING`, `APPROVED`, `REJECTED`, `REFUNDED`, `CANCELLED`. Keeps `saga_id` |

## Rules
- Money is `shopspring/decimal`, rounded to 2 places; JSON carries decimal strings (`"221.00"`).
- At most one `GENERATED` budget per service order; at most one open (`PENDING`/`APPROVED`) payment per service order (partial unique indexes).
- Payment transitions: `PENDING → APPROVED | REJECTED | CANCELLED`; `APPROVED → REFUNDED`. Anything else is `ErrInvalidTransition` (REST: 409).
- `RequestPayment` is discarded (logged, not an error) when there is no `GENERATED` budget, the amount differs from the total, or an open payment already exists.
- `RefundPayment`: `PENDING → CANCELLED` (no event); `APPROVED → REFUNDED` (+ provider refund, emits `PaymentRefunded`); other states are ignored.
- `payment.saga_id` is stored from the `RequestPayment` envelope and reused by manual `confirm`/`reject` events.
- `PaymentProvider` (`Charge`, `Refund`) is implemented by the internal provider, guided by `PAYMENT_MODE` (`manual` | `auto-approve` | `auto-reject`).

## Roles (REST)
| Role | Access |
|---|---|
| `ADMIN` | everything |
| `ATTENDANT` | `GET /v1/payments/:serviceOrderId`, `POST /v1/payments/:id/confirm|reject` |
| `MECHANIC` | `GET /v1/budgets/:serviceOrderId` |
| `CUSTOMER` | `GET /v1/budgets/:serviceOrderId` for its own budget only (other customers get 404) |

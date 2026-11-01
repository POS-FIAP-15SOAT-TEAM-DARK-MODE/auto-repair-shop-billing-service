# Domain

## Ubiquitous Language

| Code name | Business concept |
|---|---|
| `Work` | Billable service: name, description, unit price, ACTIVE/INACTIVE |
| `Supply` | Part/supply with stock quantity, unit price, optimistic-lock `Version` |
| `ServiceOrder` | Central aggregate: Customer + Vehicle + []Work + []Supply |
| `ServiceOrderHistory` | Immutable record of every status transition |
| `Customer` | Identified by CPF (individual) or CNPJ (company); linked 1:1 to a User |
| `User` | System account: name, email, bcrypt-hashed password, roles |
| `Vehicle` | License plate + brand/model/year, always linked to a Customer |

## Service Order Status Lifecycle

```
NEW → RECEIVED → IN_DIAGNOSIS → AWAITING_APPROVAL → IN_PROGRESS → COMPLETED → DELIVERED
                                        ↓
                                   REJECTED (terminal)
```

`AddWorks`, `RemoveWork`, `AddSupplies`, `RemoveSupply` require status = `NEW` → `ErrServiceOrderNotNew` otherwise.
Budget becomes immutable once status reaches `IN_PROGRESS`.

## RBAC

Roles: `ADMIN`, `ATTENDANT`, `MECHANIC`, `CUSTOMER`

| Role | Scope |
|---|---|
| `ADMIN` | Unrestricted, exclusive (cannot combine with other roles) |
| `ATTENDANT` | Customers, vehicles, service orders, works, supplies — no user management |
| `MECHANIC` | Read service orders; add/remove works and supplies; status transitions |
| `CUSTOMER` | Own service order status + own budget approve/reject (scoped by `customerId` in JWT) |

Role groups in `domain/roles.go`: `AttendantRoles`, `MechanicRoles`, `CustomerRoles`, `AttendantAndMechanicRoles`.

## User & Customer Split
- Creating a Customer automatically creates a linked User with role `CUSTOMER`.
- Default password = CPF (individual) or CNPJ (company), bcrypt-hashed (min cost 12).
- Deleting a Customer must also delete/deactivate the associated User.

## Budget Formula
```
total = Σ work.unit_price + Σ (supply.unit_price × quantity)
```
Auto-recalculate whenever works or supplies change. Immutable once status reaches `IN_PROGRESS`.

## Stock Decrement Pattern
```sql
UPDATE supply SET stock_quantity = stock_quantity - $amount, updated_at = NOW()
WHERE id = $id AND stock_quantity >= $amount
```
If `RowsAffected == 0` → return `ErrSupplyOutOfStock`. No `SELECT FOR UPDATE` — the conditional UPDATE itself prevents overselling. Stock restored on `RemoveSupply` via `RestoreStock` in the same transaction.

## Input Validation
- CPF: format `###.###.###-##` + check digit algorithm.
- CNPJ: format `##.###.###/####-##` + check digit algorithm.
- License plate: old `ABC-1234` and Mercosul `ABC1D23`.
- Monetary: `NUMERIC(10,2)` — never `float64`.

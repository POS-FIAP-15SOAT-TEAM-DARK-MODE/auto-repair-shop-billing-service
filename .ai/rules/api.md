# API

## Versioning
All endpoints under `/v1/`. Swagger UI at `http://localhost:8080/swagger/index.html`.

## Endpoints

```
POST   /auth/register                              ADMIN
POST   /auth/login                                 public
POST   /customers                                  ADMIN, ATTENDANT
GET    /customers/:id                              ADMIN, ATTENDANT
GET    /customers                                  ADMIN, ATTENDANT
PUT    /customers/:id                              ADMIN, ATTENDANT
DELETE /customers/:id                              ADMIN, ATTENDANT
POST   /works                                      ADMIN, ATTENDANT
GET    /works                                      ADMIN, ATTENDANT, MECHANIC
PUT    /works/:id                                  ADMIN, ATTENDANT, MECHANIC
DELETE /works/:id                                  ADMIN, ATTENDANT, MECHANIC
POST   /vehicles                                   ADMIN, ATTENDANT, MECHANIC
GET    /vehicles                                   ADMIN, ATTENDANT, MECHANIC  (by license plate)
PUT    /vehicles/:id                               ADMIN, ATTENDANT, MECHANIC
DELETE /vehicles/:id                               ADMIN, ATTENDANT, MECHANIC
GET    /vehicles/:customerId                       ADMIN, ATTENDANT  (list by customer)
POST   /supplies                                   ADMIN, ATTENDANT, MECHANIC
GET    /supplies                                   ADMIN, ATTENDANT, MECHANIC
PUT    /supplies/:id                               ADMIN, ATTENDANT, MECHANIC
POST   /service-order                              ADMIN, ATTENDANT
GET    /service-order/:id/history                  ADMIN, ATTENDANT
GET    /service-order/:id/services                 ADMIN, ATTENDANT
POST   /service-order/:id/services                 ADMIN, ATTENDANT
DELETE /service-order/:id/services/:serviceId      ADMIN, ATTENDANT
GET    /service-order/:id/supplies                 ADMIN, ATTENDANT
POST   /service-order/:id/supplies                 ADMIN, ATTENDANT
DELETE /service-order/:id/supplies/:supplyId       ADMIN, ATTENDANT
```

## Request/Response Conventions
- All bodies: JSON.
- Timestamps: UTC ISO 8601.
- IDs: UUID strings (ULID strings for service order IDs).
- Pagination: `?limit=N&offset=N`.

## Error Format
```json
{ "error": "Human-readable message", "code": "MACHINE_READABLE_CODE" }
```

| HTTP | Meaning |
|---|---|
| 400 | Validation error |
| 401 | Missing or invalid auth |
| 403 | Forbidden (wrong role) |
| 404 | Not found |
| 409 | Conflict |
| 422 | Business rule violation |
| 500 | Unexpected error |

## Swagger
Every endpoint must have: summary, description, request body schema, response schemas (success + errors), 401/403 documented on all protected endpoints, and role requirements noted.

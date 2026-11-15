CREATE TABLE budget (
    id               VARCHAR(36) PRIMARY KEY,
    service_order_id VARCHAR(26) NOT NULL,
    customer_user_id VARCHAR(36) NOT NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'GENERATED' CHECK (status IN ('GENERATED', 'CANCELLED')),
    total_amount     NUMERIC(10, 2) NOT NULL,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX uq_budget_active_per_so ON budget (service_order_id) WHERE status = 'GENERATED';

CREATE TABLE budget_item (
    id         VARCHAR(36) PRIMARY KEY,
    budget_id  VARCHAR(36) NOT NULL REFERENCES budget (id) ON DELETE CASCADE,
    kind       VARCHAR(10) NOT NULL CHECK (kind IN ('WORK', 'SUPPLY')),
    ref_id     VARCHAR(36) NOT NULL,
    name       VARCHAR(255) NOT NULL,
    quantity   INT NOT NULL DEFAULT 1 CHECK (quantity > 0),
    unit_price NUMERIC(10, 2) NOT NULL
);
CREATE INDEX idx_budget_item_budget_id ON budget_item (budget_id);

CREATE TABLE payment (
    id               VARCHAR(36) PRIMARY KEY,
    budget_id        VARCHAR(36) NOT NULL REFERENCES budget (id),
    service_order_id VARCHAR(26) NOT NULL,
    saga_id          VARCHAR(36) NOT NULL,
    amount           NUMERIC(10, 2) NOT NULL,
    status           VARCHAR(20) NOT NULL DEFAULT 'PENDING'
                     CHECK (status IN ('PENDING', 'APPROVED', 'REJECTED', 'REFUNDED', 'CANCELLED')),
    provider         VARCHAR(30) NOT NULL,
    created_at       TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at       TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX uq_payment_open_per_so ON payment (service_order_id) WHERE status IN ('PENDING', 'APPROVED');
CREATE INDEX idx_payment_service_order_id ON payment (service_order_id);

CREATE TABLE processed_message (
    message_id   UUID PRIMARY KEY,
    processed_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE outbox (
    id           BIGSERIAL PRIMARY KEY,
    message_id   UUID NOT NULL UNIQUE,
    topic        VARCHAR(50) NOT NULL,
    target       VARCHAR(20),
    type         VARCHAR(50) NOT NULL,
    body         JSONB NOT NULL,
    created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    published_at TIMESTAMP WITH TIME ZONE
);
CREATE INDEX idx_outbox_unpublished ON outbox (id) WHERE published_at IS NULL;

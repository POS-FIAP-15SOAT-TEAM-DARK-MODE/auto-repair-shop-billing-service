-- Approved gateway payments that Billing did not settle (the customer paid after the payment was closed,
-- or paid twice) and that must be refunded. The refund is requested by the gateway worker; the row is
-- kept as the record of what happened.
CREATE TABLE unsettled_payment (
    gateway_payment_id VARCHAR(32) PRIMARY KEY,
    payment_id         VARCHAR(36) NOT NULL REFERENCES payment (id),
    reason             VARCHAR(20) NOT NULL CHECK (reason IN ('LATE_APPROVAL', 'EXTRA_APPROVAL')),
    amount             NUMERIC(10, 2) NOT NULL,
    status             VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'REFUNDED')),
    created_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    refunded_at        TIMESTAMP WITH TIME ZONE,
    last_checked_at    TIMESTAMP WITH TIME ZONE
);
CREATE INDEX idx_unsettled_payment_pending ON unsettled_payment (last_checked_at NULLS FIRST, created_at) WHERE status = 'PENDING';

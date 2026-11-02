-- Approved gateway payments that Billing did not settle: the customer paid after the payment was closed
-- (LATE_APPROVAL) or paid twice (EXTRA_APPROVAL), which the worker refunds, or paid another amount than the
-- one charged (AMOUNT_MISMATCH), which only a person can decide (status REVIEW). The row is kept as the
-- record of what happened.
CREATE TABLE unsettled_payment (
    gateway_payment_id VARCHAR(32) PRIMARY KEY,
    payment_id         VARCHAR(36) NOT NULL REFERENCES payment (id),
    reason             VARCHAR(20) NOT NULL CHECK (reason IN ('LATE_APPROVAL', 'EXTRA_APPROVAL', 'AMOUNT_MISMATCH')),
    amount             NUMERIC(10, 2) NOT NULL,
    status             VARCHAR(20) NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING', 'REFUNDED', 'REVIEW')),
    created_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    refunded_at        TIMESTAMP WITH TIME ZONE,
    last_checked_at    TIMESTAMP WITH TIME ZONE
);
CREATE INDEX idx_unsettled_payment_pending ON unsettled_payment (last_checked_at NULLS FIRST, created_at) WHERE status = 'PENDING';

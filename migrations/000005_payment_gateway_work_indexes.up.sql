-- The worker lists the payments of each kind of work ordered by the one looked at least recently
-- (last_checked_at, never-looked-at first), then the oldest. The index of 000002 ended at created_at and
-- could not serve that ORDER BY. Refunds and closings get their own small partial indexes, so the finished
-- payments that pile up (approved, and closed ones) are never walked to find the few rows of work.
DROP INDEX IF EXISTS idx_payment_gateway_work;
CREATE INDEX idx_payment_gateway_work ON payment (status, last_checked_at NULLS FIRST, created_at, id) WHERE provider = 'mercadopago';
CREATE INDEX idx_payment_refund_work ON payment (last_checked_at NULLS FIRST, created_at, id)
    WHERE provider = 'mercadopago' AND status = 'APPROVED' AND refund_requested_at IS NOT NULL;
CREATE INDEX idx_payment_close_work ON payment (last_checked_at NULLS FIRST, created_at, id)
    WHERE provider = 'mercadopago' AND status IN ('REJECTED', 'CANCELLED', 'REFUNDED') AND checkout_closed_at IS NULL;

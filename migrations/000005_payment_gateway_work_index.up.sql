-- The worker lists the payments of each kind of work ordered by the one looked at least recently
-- (last_checked_at, never-looked-at first), then the oldest. The index of 000002 ended at created_at and
-- could not serve that ORDER BY.
DROP INDEX IF EXISTS idx_payment_gateway_work;
CREATE INDEX idx_payment_gateway_work ON payment (status, last_checked_at NULLS FIRST, created_at, id) WHERE provider = 'mercadopago';

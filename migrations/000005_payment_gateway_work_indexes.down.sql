DROP INDEX IF EXISTS idx_payment_close_work;
DROP INDEX IF EXISTS idx_payment_refund_work;
DROP INDEX IF EXISTS idx_payment_gateway_work;
CREATE INDEX idx_payment_gateway_work ON payment (status, created_at) WHERE provider = 'mercadopago';

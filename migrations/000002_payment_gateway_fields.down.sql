DROP INDEX IF EXISTS idx_payment_gateway_work;
DROP INDEX IF EXISTS uq_payment_external_id;

ALTER TABLE payment
    DROP COLUMN IF EXISTS checkout_closed_at,
    DROP COLUMN IF EXISTS refund_requested_at,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS checkout_url,
    DROP COLUMN IF EXISTS gateway_payment_id,
    DROP COLUMN IF EXISTS external_id;

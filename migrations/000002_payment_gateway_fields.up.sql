-- Fields used by the Mercado Pago provider. Payments of the internal provider leave them NULL.
ALTER TABLE payment
    ADD COLUMN external_id          VARCHAR(64),
    ADD COLUMN gateway_payment_id   VARCHAR(32),
    ADD COLUMN checkout_url         TEXT,
    ADD COLUMN expires_at           TIMESTAMP WITH TIME ZONE,
    ADD COLUMN refund_requested_at  TIMESTAMP WITH TIME ZONE,
    ADD COLUMN checkout_closed_at   TIMESTAMP WITH TIME ZONE;

CREATE UNIQUE INDEX uq_payment_external_id ON payment (external_id) WHERE external_id IS NOT NULL;
CREATE INDEX idx_payment_gateway_work ON payment (status, created_at) WHERE provider = 'mercadopago';

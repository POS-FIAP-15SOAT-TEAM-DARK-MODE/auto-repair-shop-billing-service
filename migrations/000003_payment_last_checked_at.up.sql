-- When the gateway worker last looked at a payment, so every payment gets its turn and a slow or
-- failing one cannot keep the others out of the batch.
ALTER TABLE payment ADD COLUMN last_checked_at TIMESTAMP WITH TIME ZONE;

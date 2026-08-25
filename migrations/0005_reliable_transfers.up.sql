ALTER TABLE customers ADD COLUMN requested_initial_deposit_minor BIGINT;
UPDATE customers SET requested_initial_deposit_minor = ROUND(requested_initial_deposit * 100);
ALTER TABLE customers ALTER COLUMN requested_initial_deposit_minor SET NOT NULL;
ALTER TABLE customers DROP COLUMN requested_initial_deposit;
ALTER TABLE customers RENAME COLUMN requested_initial_deposit_minor TO requested_initial_deposit;

CREATE TABLE transfers (
    id TEXT PRIMARY KEY,
    source_account_id TEXT NOT NULL,
    destination_account_id TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    amount_minor BIGINT NOT NULL,
    fee_minor BIGINT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('pending','completed','failed')),
    failure_code TEXT NOT NULL DEFAULT '',
    initial_source_balance_minor BIGINT NOT NULL DEFAULT 0,
    final_source_balance_minor BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (source_account_id, idempotency_key)
);

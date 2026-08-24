ALTER TABLE customers ADD COLUMN password_hash TEXT NOT NULL DEFAULT '';
CREATE UNIQUE INDEX customers_email_unique ON customers (lower(email));

CREATE TABLE sessions (
    token_hash  TEXT        PRIMARY KEY,
    customer_id TEXT        NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    account_id  TEXT        NOT NULL,
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

ALTER TABLE customers ALTER COLUMN account_id DROP NOT NULL;
ALTER TABLE customers ADD COLUMN requested_initial_deposit DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE customers ADD COLUMN reviewed_by TEXT;
ALTER TABLE customers ADD COLUMN reviewed_at TIMESTAMPTZ;
ALTER TABLE customers ADD COLUMN rejection_reason TEXT NOT NULL DEFAULT '';
UPDATE customers SET kyc_status = 'approved' WHERE kyc_status = 'verified';

CREATE TABLE admins (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX admins_email_unique ON admins (lower(email));
ALTER TABLE customers ADD CONSTRAINT customers_reviewer_fk FOREIGN KEY (reviewed_by) REFERENCES admins(id);

ALTER TABLE sessions ALTER COLUMN customer_id DROP NOT NULL;
ALTER TABLE sessions ALTER COLUMN account_id DROP NOT NULL;
ALTER TABLE sessions ADD COLUMN admin_id TEXT REFERENCES admins(id) ON DELETE CASCADE;
ALTER TABLE sessions ADD CONSTRAINT sessions_identity_check CHECK (
    (customer_id IS NOT NULL AND admin_id IS NULL) OR
    (customer_id IS NULL AND admin_id IS NOT NULL)
);

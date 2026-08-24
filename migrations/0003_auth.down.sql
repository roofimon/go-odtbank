DROP TABLE IF EXISTS sessions;
DROP INDEX IF EXISTS customers_email_unique;
ALTER TABLE customers DROP COLUMN IF EXISTS password_hash;

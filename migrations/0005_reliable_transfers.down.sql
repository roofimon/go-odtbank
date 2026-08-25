DROP TABLE IF EXISTS transfers;
ALTER TABLE customers ADD COLUMN requested_initial_deposit_major DOUBLE PRECISION;
UPDATE customers SET requested_initial_deposit_major = requested_initial_deposit::DOUBLE PRECISION / 100;
ALTER TABLE customers ALTER COLUMN requested_initial_deposit_major SET NOT NULL;
ALTER TABLE customers DROP COLUMN requested_initial_deposit;
ALTER TABLE customers RENAME COLUMN requested_initial_deposit_major TO requested_initial_deposit;

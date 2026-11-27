-- Drop the filters column added in 0004_webhook_filters.up.sql.
-- SQLite 3.35+ DROP COLUMN (supported by modernc.org/sqlite).

ALTER TABLE webhooks DROP COLUMN filters;

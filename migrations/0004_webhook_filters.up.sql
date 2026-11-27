-- 0004_webhook_filters: optional category/tag filters on webhooks (v0.2).
-- Empty arrays (or an empty JSON object) mean "match every post" — same
-- behaviour as before this column existed.

ALTER TABLE webhooks ADD COLUMN filters TEXT NOT NULL DEFAULT '{}';

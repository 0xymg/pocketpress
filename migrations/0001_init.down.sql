-- 0001_init.down: drop every table created by 0001_init.up.sql, in
-- FK-safe order (children before the parents they reference). Dropping a
-- table also drops the indexes defined on it, so no separate DROP INDEX
-- statements are needed.

DROP TABLE IF EXISTS webhook_deliveries;
DROP TABLE IF EXISTS webhooks;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS post_revisions;
DROP TABLE IF EXISTS post_tags;
DROP TABLE IF EXISTS post_categories;
DROP TABLE IF EXISTS posts;
DROP TABLE IF EXISTS tags;
DROP TABLE IF EXISTS categories;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;

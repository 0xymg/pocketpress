-- 0005_author_profiles down: drop profile columns added for richer authors.

DROP INDEX IF EXISTS idx_users_slug;

-- SQLite cannot DROP COLUMN before 3.35; PocketPress targets modern SQLite
-- (modernc.org/sqlite) which supports it.
ALTER TABLE users DROP COLUMN location;
ALTER TABLE users DROP COLUMN website;
ALTER TABLE users DROP COLUMN bio;
ALTER TABLE users DROP COLUMN slug;

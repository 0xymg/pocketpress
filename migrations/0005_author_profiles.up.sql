-- 0005_author_profiles: richer public author profiles for multi-author blogs (v0.3).
-- Existing users get a unique slug seeded from their id (UUIDv7 is URL-safe);
-- the application generates name-based slugs for new users and allows edits.

ALTER TABLE users ADD COLUMN slug TEXT;
ALTER TABLE users ADD COLUMN bio TEXT;
ALTER TABLE users ADD COLUMN website TEXT;
ALTER TABLE users ADD COLUMN location TEXT;

UPDATE users SET slug = id WHERE slug IS NULL OR slug = '';

CREATE UNIQUE INDEX idx_users_slug ON users (slug);

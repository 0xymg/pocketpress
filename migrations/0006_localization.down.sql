-- Reverse 0006_localization.

DROP INDEX IF EXISTS idx_posts_translation_group_locale;
DROP INDEX IF EXISTS idx_posts_translation_group;
DROP INDEX IF EXISTS idx_posts_locale;

ALTER TABLE posts DROP COLUMN translation_group;
ALTER TABLE posts DROP COLUMN locale;

ALTER TABLE settings DROP COLUMN available_locales;

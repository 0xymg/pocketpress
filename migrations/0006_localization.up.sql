-- 0006_localization: per-post locales and translation groups (v0.3).
-- settings.locale remains the site default; available_locales lists
-- locales the admin may assign to posts. Existing posts default to 'en'.

ALTER TABLE posts ADD COLUMN locale TEXT NOT NULL DEFAULT 'en';
ALTER TABLE posts ADD COLUMN translation_group TEXT;

CREATE INDEX idx_posts_locale ON posts (locale);
CREATE INDEX idx_posts_translation_group ON posts (translation_group)
    WHERE translation_group IS NOT NULL;
-- At most one post per locale within a translation group.
CREATE UNIQUE INDEX idx_posts_translation_group_locale
    ON posts (translation_group, locale)
    WHERE translation_group IS NOT NULL;

ALTER TABLE settings ADD COLUMN available_locales TEXT NOT NULL DEFAULT '["en"]';

-- Seed available_locales from the existing site default when it isn't "en".
UPDATE settings
SET available_locales = '["' || replace(locale, '"', '') || '"]'
WHERE locale IS NOT NULL AND locale <> '' AND locale <> 'en';

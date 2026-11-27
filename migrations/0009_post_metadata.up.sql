-- 0009_post_metadata: per-post custom metadata JSON object (v0.3).
-- Free-form object for product-specific fields; never replaces SEO columns.

ALTER TABLE posts ADD COLUMN metadata TEXT NOT NULL DEFAULT '{}';

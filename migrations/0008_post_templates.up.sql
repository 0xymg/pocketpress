-- 0008_post_templates: reusable content duplication templates (v0.3).
-- Templates store a snapshot of post body/metadata used to mint new drafts.
-- They are admin-only authoring aids and never appear in the public API.

CREATE TABLE post_templates (
    id               TEXT PRIMARY KEY,
    name             TEXT NOT NULL,
    description      TEXT,
    title            TEXT NOT NULL DEFAULT '',
    excerpt          TEXT,
    content_json     TEXT NOT NULL DEFAULT '{}',
    content_html     TEXT NOT NULL DEFAULT '',
    content_text     TEXT NOT NULL DEFAULT '',
    cover_image      TEXT,
    featured         INTEGER NOT NULL DEFAULT 0 CHECK (featured IN (0, 1)),
    seo_title        TEXT,
    seo_description  TEXT,
    canonical_url    TEXT,
    og_image         TEXT,
    locale           TEXT NOT NULL DEFAULT 'en',
    created_by       TEXT NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);

CREATE INDEX idx_post_templates_name ON post_templates (name COLLATE NOCASE);
CREATE INDEX idx_post_templates_updated_at ON post_templates (updated_at);

CREATE TABLE post_template_categories (
    template_id  TEXT NOT NULL REFERENCES post_templates (id) ON DELETE CASCADE,
    category_id  TEXT NOT NULL REFERENCES categories (id) ON DELETE CASCADE,
    PRIMARY KEY (template_id, category_id)
);

CREATE INDEX idx_post_template_categories_category_id
    ON post_template_categories (category_id);

CREATE TABLE post_template_tags (
    template_id  TEXT NOT NULL REFERENCES post_templates (id) ON DELETE CASCADE,
    tag_id       TEXT NOT NULL REFERENCES tags (id) ON DELETE CASCADE,
    PRIMARY KEY (template_id, tag_id)
);

CREATE INDEX idx_post_template_tags_tag_id ON post_template_tags (tag_id);

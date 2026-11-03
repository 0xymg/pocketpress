-- 0002_media: the media table (Phase 4 — rich-text editor and media),
-- backing uploaded images. IDs are TEXT UUIDv7 values and created_at is a
-- TEXT RFC3339 (UTC) value, both set by the application, matching every
-- other table in this schema.
--
-- posts.cover_image and posts.og_image (added in 0001_init as plain TEXT
-- columns) store a media.id reference; there is no FOREIGN KEY on them
-- because a post may reference media created after the posts table itself
-- and the media/content services enforce the reference server-side (see
-- internal/content's checkMedia and the media_in_use delete guard).

CREATE TABLE media (
    id             TEXT PRIMARY KEY,
    file_key       TEXT NOT NULL UNIQUE,
    original_name  TEXT NOT NULL,
    mime           TEXT NOT NULL,
    size           INTEGER NOT NULL,
    width          INTEGER,
    height         INTEGER,
    alt            TEXT NOT NULL DEFAULT '',
    storage        TEXT NOT NULL DEFAULT 'local',
    uploaded_by    TEXT REFERENCES users (id) ON DELETE SET NULL,
    created_at     TEXT NOT NULL
);

CREATE INDEX idx_media_created_at ON media (created_at);

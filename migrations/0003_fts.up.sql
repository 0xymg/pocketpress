-- 0003_fts: SQLite FTS5 index over posts for full-text search (v0.2).
-- External-content table mirrors posts.rowid so MATCH queries join back to
-- the posts row without duplicating the body. Triggers keep the index in
-- sync on insert/update/delete; the INSERT…SELECT backfills existing rows.

CREATE VIRTUAL TABLE posts_fts USING fts5(
    title,
    excerpt,
    content_text,
    content = 'posts',
    content_rowid = 'rowid',
    tokenize = 'porter unicode61 remove_diacritics 2'
);

CREATE TRIGGER posts_fts_ai AFTER INSERT ON posts BEGIN
    INSERT INTO posts_fts(rowid, title, excerpt, content_text)
    VALUES (new.rowid, new.title, coalesce(new.excerpt, ''), new.content_text);
END;

CREATE TRIGGER posts_fts_ad AFTER DELETE ON posts BEGIN
    INSERT INTO posts_fts(posts_fts, rowid, title, excerpt, content_text)
    VALUES ('delete', old.rowid, old.title, coalesce(old.excerpt, ''), old.content_text);
END;

CREATE TRIGGER posts_fts_au AFTER UPDATE OF title, excerpt, content_text ON posts BEGIN
    INSERT INTO posts_fts(posts_fts, rowid, title, excerpt, content_text)
    VALUES ('delete', old.rowid, old.title, coalesce(old.excerpt, ''), old.content_text);
    INSERT INTO posts_fts(rowid, title, excerpt, content_text)
    VALUES (new.rowid, new.title, coalesce(new.excerpt, ''), new.content_text);
END;

INSERT INTO posts_fts(rowid, title, excerpt, content_text)
SELECT rowid, title, coalesce(excerpt, ''), content_text FROM posts;

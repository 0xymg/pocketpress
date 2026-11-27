-- 0003_fts down: drop FTS sync triggers and the posts_fts virtual table
-- (shadow tables go with it).

DROP TRIGGER IF EXISTS posts_fts_ai;
DROP TRIGGER IF EXISTS posts_fts_ad;
DROP TRIGGER IF EXISTS posts_fts_au;
DROP TABLE IF EXISTS posts_fts;

-- 0009_post_metadata down: drop the custom metadata column.

ALTER TABLE posts DROP COLUMN metadata;

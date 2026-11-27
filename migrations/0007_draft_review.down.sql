-- Revert 0007_draft_review.

ALTER TABLE settings DROP COLUMN require_review_before_publish;

DROP INDEX IF EXISTS idx_posts_review_status;

ALTER TABLE posts DROP COLUMN submitted_for_review_at;
ALTER TABLE posts DROP COLUMN reviewed_at;
ALTER TABLE posts DROP COLUMN reviewed_by;
ALTER TABLE posts DROP COLUMN review_note;
ALTER TABLE posts DROP COLUMN review_status;

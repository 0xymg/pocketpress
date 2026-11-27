-- 0007_draft_review: editorial draft review workflow (v0.3).
-- review_status is orthogonal to publication status (draft/scheduled/
-- published/archived). When settings.require_review_before_publish is on,
-- publish/schedule require review_status = 'approved'.

ALTER TABLE posts ADD COLUMN review_status TEXT NOT NULL DEFAULT 'none'
    CHECK (review_status IN ('none', 'pending', 'approved', 'changes_requested'));
ALTER TABLE posts ADD COLUMN review_note TEXT;
ALTER TABLE posts ADD COLUMN reviewed_by TEXT REFERENCES users (id) ON DELETE SET NULL;
ALTER TABLE posts ADD COLUMN reviewed_at TEXT;
ALTER TABLE posts ADD COLUMN submitted_for_review_at TEXT;

CREATE INDEX idx_posts_review_status ON posts (review_status);

ALTER TABLE settings ADD COLUMN require_review_before_publish INTEGER NOT NULL DEFAULT 0
    CHECK (require_review_before_publish IN (0, 1));

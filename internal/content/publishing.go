package content

import (
	"context"
	"log/slog"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// Frozen validation messages for the publish-state operations. The invalid-
// transition messages surface as details.status; the past-schedule message
// surfaces as details.scheduled_for.
const (
	scheduleNotFutureMessage   = "Scheduled time must be in the future."
	unpublishNotAllowedMessage = "Only a published, scheduled, or archived post can be moved back to draft."
	scheduleNotAllowedMessage  = "A published post cannot be scheduled; move it back to draft first."
)

// PublishPost transitions a post to the published status. published_at is set
// to now only if it is currently empty — returning a post to draft never
// erases it, and re-publishing keeps the original first-published timestamp.
// Any scheduled_for is cleared (a published post is no longer scheduled).
//
// Publishing is allowed from any status; publishing an already-published post
// is an idempotent no-op that returns the post unchanged (and fires no
// event). Returns store.ErrNotFound for an unknown id.
func (s *Service) PublishPost(ctx context.Context, id string) (Post, error) {
	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	if existing.Status == store.PostStatusPublished {
		// Already live: keep published_at, change nothing, fire nothing.
		return s.loadPost(ctx, id)
	}

	publishedAt := existing.PublishedAt
	if publishedAt == nil {
		now := time.Now().UTC()
		publishedAt = &now
	}
	if err := store.SetPostStatus(ctx, s.db, id, store.PostStatusPublished, publishedAt, nil); err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	s.logger.Info("post published", slog.String("post_id", id))
	s.events.PostPublished(ctx, &p)
	return p, nil
}

// UnpublishPost moves a post back to draft. published_at is preserved (so a
// later re-publish keeps the original) and scheduled_for is cleared. It is
// allowed from published, scheduled or archived; unpublishing a draft is a
// validation error (details.status). Only a previously-published post fires
// PostUnpublished — a scheduled or archived post was never publicly live.
// Returns store.ErrNotFound for an unknown id.
func (s *Service) UnpublishPost(ctx context.Context, id string) (Post, error) {
	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	switch existing.Status {
	case store.PostStatusPublished, store.PostStatusScheduled, store.PostStatusArchived:
	default:
		return Post{}, &ValidationError{Fields: map[string]string{"status": unpublishNotAllowedMessage}}
	}

	if err := store.SetPostStatus(ctx, s.db, id, store.PostStatusDraft, existing.PublishedAt, nil); err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	s.logger.Info("post unpublished", slog.String("post_id", id))
	if existing.Status == store.PostStatusPublished {
		s.events.PostUnpublished(ctx, &p)
	}
	return p, nil
}

// SchedulePost transitions a post to the scheduled status with the given
// scheduled_for, which must be strictly in the future (compared in UTC) —
// otherwise a validation error on scheduled_for. It is allowed from draft,
// scheduled (re-schedule) or archived; scheduling a published post is a
// validation error on status. published_at is left untouched (a post that was
// once published keeps its original timestamp). Returns store.ErrNotFound for
// an unknown id.
func (s *Service) SchedulePost(ctx context.Context, id string, scheduledFor time.Time) (Post, error) {
	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	switch existing.Status {
	case store.PostStatusDraft, store.PostStatusScheduled, store.PostStatusArchived:
	default:
		return Post{}, &ValidationError{Fields: map[string]string{"status": scheduleNotAllowedMessage}}
	}

	scheduledFor = scheduledFor.UTC()
	if !scheduledFor.After(time.Now().UTC()) {
		return Post{}, &ValidationError{Fields: map[string]string{"scheduled_for": scheduleNotFutureMessage}}
	}

	if err := store.SetPostStatus(ctx, s.db, id, store.PostStatusScheduled, existing.PublishedAt, &scheduledFor); err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	s.logger.Info("post scheduled", slog.String("post_id", id), slog.String("scheduled_for", scheduledFor.Format(time.RFC3339)))
	return p, nil
}

// ArchivePost transitions a post to the archived status from any status
// (archiving an already-archived post is an idempotent no-op). published_at
// and scheduled_for are both preserved. Archiving a currently-published post
// ends its public visibility, so it fires PostUnpublished (archiving a draft or
// scheduled post — never publicly live as "published" — fires nothing).
// Returns store.ErrNotFound for an unknown id.
func (s *Service) ArchivePost(ctx context.Context, id string) (Post, error) {
	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	if existing.Status == store.PostStatusArchived {
		return s.loadPost(ctx, id)
	}

	if err := store.SetPostStatus(ctx, s.db, id, store.PostStatusArchived, existing.PublishedAt, existing.ScheduledFor); err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	s.logger.Info("post archived", slog.String("post_id", id))
	if existing.Status == store.PostStatusPublished {
		s.events.PostUnpublished(ctx, &p)
	}
	return p, nil
}

// PromoteScheduledPosts publishes every scheduled post whose scheduled_for is
// at or before now: status becomes published, published_at becomes the
// intended scheduled_for (unless the post already carries an earlier
// published_at, which is kept — the set-once rule), and scheduled_for is
// cleared. Each promotion is logged and fires PostPublished. It returns the
// promoted post aggregates. The scheduled-publish worker calls this on a
// ticker and once at startup (restart survival).
func (s *Service) PromoteScheduledPosts(ctx context.Context, now time.Time) ([]Post, error) {
	due, err := store.ListDuePosts(ctx, s.db, now)
	if err != nil {
		return nil, err
	}

	promoted := make([]Post, 0, len(due))
	for _, row := range due {
		publishedAt := row.PublishedAt
		if publishedAt == nil {
			// The intended publish time is scheduled_for, not the wall clock at
			// promotion — a post that came due while the process was down is
			// dated to when it should have gone live.
			publishedAt = row.ScheduledFor
		}
		if err := store.SetPostStatus(ctx, s.db, row.ID, store.PostStatusPublished, publishedAt, nil); err != nil {
			return promoted, err
		}
		p, err := s.loadPost(ctx, row.ID)
		if err != nil {
			return promoted, err
		}
		s.logger.Info("scheduled post promoted to published",
			slog.String("post_id", row.ID), slog.String("slug", row.Slug))
		s.events.PostPublished(ctx, &p)
		promoted = append(promoted, p)
	}
	return promoted, nil
}

// GetPostBySlugForPreview loads a post by slug regardless of its status, for
// the authorized preview endpoint (which shows drafts, scheduled and archived
// posts to a caller holding a valid preview token). Returns store.ErrNotFound
// when no post has the slug.
func (s *Service) GetPostBySlugForPreview(ctx context.Context, slug string) (Post, error) {
	row, err := store.GetPostBySlug(ctx, s.db, slug)
	if err != nil {
		return Post{}, err
	}
	return s.hydrate(ctx, row, nil)
}

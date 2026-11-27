package content

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// Frozen validation messages for draft-review transitions.
const (
	submitReviewNotAllowedMessage      = "Only a draft or archived post can be submitted for review."
	submitReviewBadStateMessage        = "Post is already pending review or approved."
	approveReviewNotPendingMessage     = "Only a post pending review can be approved."
	requestChangesNotPendingMessage    = "Only a post pending review can receive change requests."
	requestChangesNoteRequiredMessage  = "A note is required when requesting changes."
	withdrawReviewNotPendingMessage    = "Only a post pending review can be withdrawn."
	reviewRequiredBeforePublishMessage = "This post must be approved before it can be published or scheduled."
)

// SubmitForReview marks a draft/archived post as pending review. Allowed
// from review_status none or changes_requested. Clears prior review note /
// reviewed_by / reviewed_at and stamps submitted_for_review_at.
func (s *Service) SubmitForReview(ctx context.Context, id string) (Post, error) {
	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	switch existing.Status {
	case store.PostStatusDraft, store.PostStatusArchived:
	default:
		return Post{}, &ValidationError{Fields: map[string]string{"status": submitReviewNotAllowedMessage}}
	}
	switch existing.ReviewStatus {
	case store.ReviewStatusNone, store.ReviewStatusChangesRequested:
	default:
		return Post{}, &ValidationError{Fields: map[string]string{"review_status": submitReviewBadStateMessage}}
	}

	now := time.Now().UTC()
	if err := store.SetPostReview(ctx, s.db, id, store.PostReviewUpdate{
		ReviewStatus:         store.ReviewStatusPending,
		SubmittedForReviewAt: &now,
	}); err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	s.logger.Info("post submitted for review", slog.String("post_id", id))
	return p, nil
}

// ApproveReview marks a pending post as approved. reviewerID is recorded
// on the post; the review note is cleared.
func (s *Service) ApproveReview(ctx context.Context, id, reviewerID string) (Post, error) {
	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	if existing.ReviewStatus != store.ReviewStatusPending {
		return Post{}, &ValidationError{Fields: map[string]string{"review_status": approveReviewNotPendingMessage}}
	}

	now := time.Now().UTC()
	if err := store.SetPostReview(ctx, s.db, id, store.PostReviewUpdate{
		ReviewStatus:         store.ReviewStatusApproved,
		ReviewedBy:           reviewerID,
		ReviewedAt:           &now,
		SubmittedForReviewAt: existing.SubmittedForReviewAt,
	}); err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	s.logger.Info("post review approved", slog.String("post_id", id), slog.String("reviewer_id", reviewerID))
	return p, nil
}

// RequestChanges marks a pending post as changes_requested with a required
// note from the reviewer.
func (s *Service) RequestChanges(ctx context.Context, id, reviewerID, note string) (Post, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return Post{}, &ValidationError{Fields: map[string]string{"note": requestChangesNoteRequiredMessage}}
	}

	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	if existing.ReviewStatus != store.ReviewStatusPending {
		return Post{}, &ValidationError{Fields: map[string]string{"review_status": requestChangesNotPendingMessage}}
	}

	now := time.Now().UTC()
	if err := store.SetPostReview(ctx, s.db, id, store.PostReviewUpdate{
		ReviewStatus:         store.ReviewStatusChangesRequested,
		ReviewNote:           note,
		ReviewedBy:           reviewerID,
		ReviewedAt:           &now,
		SubmittedForReviewAt: existing.SubmittedForReviewAt,
	}); err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	s.logger.Info("post review changes requested", slog.String("post_id", id), slog.String("reviewer_id", reviewerID))
	return p, nil
}

// WithdrawReview returns a pending post to review_status none, clearing
// submission and review metadata.
func (s *Service) WithdrawReview(ctx context.Context, id string) (Post, error) {
	existing, err := store.GetPostByID(ctx, s.db, id)
	if err != nil {
		return Post{}, err
	}
	if existing.ReviewStatus != store.ReviewStatusPending {
		return Post{}, &ValidationError{Fields: map[string]string{"review_status": withdrawReviewNotPendingMessage}}
	}

	if err := store.SetPostReview(ctx, s.db, id, store.PostReviewUpdate{
		ReviewStatus: store.ReviewStatusNone,
	}); err != nil {
		return Post{}, err
	}

	p, err := s.loadPost(ctx, id)
	if err != nil {
		return Post{}, err
	}
	s.logger.Info("post review withdrawn", slog.String("post_id", id))
	return p, nil
}

// requireReviewApproved returns a validation error when the site requires
// review before publish/schedule and the post is not approved. Missing
// settings (pre-setup / unit fixtures without a settings row) are treated
// as the gate being off.
func (s *Service) requireReviewApproved(ctx context.Context, reviewStatus string) error {
	settings, err := store.GetSettings(ctx, s.db)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	if !settings.RequireReviewBeforePublish {
		return nil
	}
	if reviewStatus == store.ReviewStatusApproved {
		return nil
	}
	return &ValidationError{Fields: map[string]string{"review_status": reviewRequiredBeforePublishMessage}}
}

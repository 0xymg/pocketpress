package http

import (
	"net/http"
	"testing"
	"time"
)

func TestDraftReviewHappyPath(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	id := env.makePost(t, "Review me", "review-me", "draft")
	post := env.mustGet(t, id)
	if post["review_status"] != "none" {
		t.Fatalf("initial review_status = %v, want none", post["review_status"])
	}

	post = env.mustAction(t, id, "submit-review", nil)
	if post["review_status"] != "pending" {
		t.Fatalf("after submit: review_status = %v, want pending", post["review_status"])
	}
	if post["submitted_for_review_at"] == nil {
		t.Fatal("after submit: submitted_for_review_at is null")
	}

	post = env.mustAction(t, id, "approve-review", nil)
	if post["review_status"] != "approved" {
		t.Fatalf("after approve: review_status = %v, want approved", post["review_status"])
	}
	if post["reviewed_by"] == nil {
		t.Fatal("after approve: reviewed_by is null")
	}
	if post["reviewed_at"] == nil {
		t.Fatal("after approve: reviewed_at is null")
	}
}

func TestDraftReviewRequestChangesAndResubmit(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	id := env.makePost(t, "Needs work", "needs-work", "draft")
	env.mustAction(t, id, "submit-review", nil)

	resp, m := env.action(t, id, "request-changes", map[string]any{})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("request-changes without note = %d, want 400; body = %v", resp.StatusCode, m)
	}

	post := env.mustAction(t, id, "request-changes", map[string]any{"note": "Tighten the intro."})
	if post["review_status"] != "changes_requested" {
		t.Fatalf("after request-changes: review_status = %v", post["review_status"])
	}
	if post["review_note"] != "Tighten the intro." {
		t.Fatalf("review_note = %v", post["review_note"])
	}

	post = env.mustAction(t, id, "submit-review", nil)
	if post["review_status"] != "pending" {
		t.Fatalf("resubmit: review_status = %v, want pending", post["review_status"])
	}
	if note, _ := post["review_note"].(string); note != "" {
		t.Fatalf("resubmit should clear review_note, got %q", note)
	}
}

func TestDraftReviewWithdraw(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	id := env.makePost(t, "Withdraw me", "withdraw-me", "draft")
	env.mustAction(t, id, "submit-review", nil)
	post := env.mustAction(t, id, "withdraw-review", nil)
	if post["review_status"] != "none" {
		t.Fatalf("after withdraw: review_status = %v, want none", post["review_status"])
	}
	if post["submitted_for_review_at"] != nil {
		t.Fatalf("after withdraw: submitted_for_review_at = %v, want null", post["submitted_for_review_at"])
	}
}

func TestDraftReviewInvalidTransitions(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	id := env.makePost(t, "Invalid", "invalid-review", "draft")

	resp, m := env.action(t, id, "approve-review", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("approve when none = %d, want 400; body = %v", resp.StatusCode, m)
	}

	env.mustAction(t, id, "submit-review", nil)
	resp, m = env.action(t, id, "submit-review", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("double submit = %d, want 400; body = %v", resp.StatusCode, m)
	}

	pubID := env.makePost(t, "Live", "live-no-review", "published")
	resp, m = env.action(t, pubID, "submit-review", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("submit published = %d, want 400; body = %v", resp.StatusCode, m)
	}
}

func TestRequireReviewBeforePublishAndSchedule(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodPatch, "/api/admin/v1/settings", map[string]any{
		"require_review_before_publish": true,
	})
	if resp.StatusCode != http.StatusOK {
		_, m := readJSON(t, resp)
		t.Fatalf("enable require_review = %d; body = %v", resp.StatusCode, m)
	}

	id := env.makePost(t, "Gate me", "gate-me", "draft")
	resp, m := env.action(t, id, "publish", nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("publish without approval = %d, want 400; body = %v", resp.StatusCode, m)
	}
	details := validationDetails(t, m)
	if details["review_status"] == "" {
		t.Fatalf("expected review_status validation detail, got %v", details)
	}

	resp, m = env.action(t, id, "schedule", map[string]any{
		"scheduled_for": futureRFC3339(time.Hour),
	})
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("schedule without approval = %d, want 400; body = %v", resp.StatusCode, m)
	}

	env.mustAction(t, id, "submit-review", nil)
	env.mustAction(t, id, "approve-review", nil)
	post := env.mustAction(t, id, "publish", nil)
	if post["status"] != "published" {
		t.Fatalf("publish after approve: status = %v", post["status"])
	}

	id2 := env.makePost(t, "Sched gate", "sched-gate", "draft")
	env.mustAction(t, id2, "submit-review", nil)
	env.mustAction(t, id2, "approve-review", nil)
	post = env.mustAction(t, id2, "schedule", map[string]any{
		"scheduled_for": futureRFC3339(time.Hour),
	})
	if post["status"] != "scheduled" {
		t.Fatalf("schedule after approve: status = %v", post["status"])
	}
}

func TestListPostsByReviewStatus(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	a := env.makePost(t, "Pending A", "pending-a", "draft")
	_ = env.makePost(t, "None B", "none-b", "draft")
	env.mustAction(t, a, "submit-review", nil)

	resp := env.do(t, http.MethodGet, "/api/admin/v1/posts?review_status=pending", nil)
	if resp.StatusCode != http.StatusOK {
		_, m := readJSON(t, resp)
		t.Fatalf("list pending = %d; body = %v", resp.StatusCode, m)
	}
	_, m := readJSON(t, resp)
	posts, _ := m["posts"].([]any)
	if len(posts) != 1 {
		t.Fatalf("pending list len = %d, want 1; body = %v", len(posts), m)
	}
	row := posts[0].(map[string]any)
	if row["id"] != a {
		t.Fatalf("pending list id = %v, want %s", row["id"], a)
	}
}

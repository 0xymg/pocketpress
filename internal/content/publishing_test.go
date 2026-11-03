package content

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

// fakeEvents records every event the service fires, for assertions.
type fakeEvents struct {
	published   []string
	unpublished []string
	updated     []string
	deleted     []string
}

func (f *fakeEvents) PostPublished(_ context.Context, p *Post) {
	f.published = append(f.published, p.Row.ID)
}
func (f *fakeEvents) PostUnpublished(_ context.Context, p *Post) {
	f.unpublished = append(f.unpublished, p.Row.ID)
}
func (f *fakeEvents) PostUpdated(_ context.Context, p *Post) { f.updated = append(f.updated, p.Row.ID) }
func (f *fakeEvents) PostDeleted(_ context.Context, p *Post) { f.deleted = append(f.deleted, p.Row.ID) }

// serviceEnv is a content Service over a fresh temp-dir store with a recording
// event sink and a ready-made author.
type serviceEnv struct {
	svc      *Service
	events   *fakeEvents
	db       *sql.DB
	authorID string
}

func newServiceEnv(t *testing.T) *serviceEnv {
	t.Helper()
	ctx := context.Background()

	dataDir := t.TempDir()
	st, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	if _, err := store.Up(ctx, st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	author, err := store.CreateUser(ctx, st.DB(), store.NewUser{
		Email:        "author@example.com",
		PasswordHash: "x",
		Name:         "Author",
		Role:         store.RoleOwner,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	svc := NewService(st.DB(), nil)
	rec := &fakeEvents{}
	svc.SetEvents(rec)

	return &serviceEnv{svc: svc, events: rec, db: st.DB(), authorID: author.ID}
}

// newPost creates a draft post and returns its id.
func (e *serviceEnv) newPost(t *testing.T, title string) string {
	t.Helper()
	p, err := e.svc.CreatePost(context.Background(), PostCreateInput{Title: title, CurrentUserID: e.authorID})
	if err != nil {
		t.Fatalf("CreatePost(%q) error = %v", title, err)
	}
	return p.Row.ID
}

// row reads a post row straight from the store.
func (e *serviceEnv) row(t *testing.T, id string) store.Post {
	t.Helper()
	p, err := store.GetPostByID(context.Background(), e.db, id)
	if err != nil {
		t.Fatalf("GetPostByID(%s) error = %v", id, err)
	}
	return p
}

// forceState sets a post's status/published_at/scheduled_for directly, for
// seeding states (e.g. an overdue scheduled post) the public API can't reach.
func (e *serviceEnv) forceState(t *testing.T, id, status string, publishedAt, scheduledFor *time.Time) {
	t.Helper()
	if err := store.SetPostStatus(context.Background(), e.db, id, status, publishedAt, scheduledFor); err != nil {
		t.Fatalf("SetPostStatus(%s) error = %v", id, err)
	}
}

// ---------------------------------------------------------------------------
// published_at set-once
// ---------------------------------------------------------------------------

// TestPublishedAtSetOnce confirms publish sets published_at once; unpublish
// preserves it; and a re-publish keeps the original first-published time.
func TestPublishedAtSetOnce(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()
	id := env.newPost(t, "Set Once")

	p, err := env.svc.PublishPost(ctx, id)
	if err != nil {
		t.Fatalf("PublishPost() error = %v", err)
	}
	if p.Row.Status != store.PostStatusPublished || p.Row.PublishedAt == nil {
		t.Fatalf("after publish status=%q publishedAt=%v, want published + set", p.Row.Status, p.Row.PublishedAt)
	}
	first := *p.Row.PublishedAt

	// Unpublish preserves published_at, clears scheduled_for, goes to draft.
	p, err = env.svc.UnpublishPost(ctx, id)
	if err != nil {
		t.Fatalf("UnpublishPost() error = %v", err)
	}
	if p.Row.Status != store.PostStatusDraft {
		t.Fatalf("after unpublish status = %q, want draft", p.Row.Status)
	}
	if p.Row.PublishedAt == nil || !p.Row.PublishedAt.Equal(first) {
		t.Fatalf("after unpublish publishedAt = %v, want preserved %v", p.Row.PublishedAt, first)
	}

	// Re-publish keeps the original published_at.
	p, err = env.svc.PublishPost(ctx, id)
	if err != nil {
		t.Fatalf("re-PublishPost() error = %v", err)
	}
	if p.Row.PublishedAt == nil || !p.Row.PublishedAt.Equal(first) {
		t.Fatalf("after re-publish publishedAt = %v, want original %v", p.Row.PublishedAt, first)
	}
}

// ---------------------------------------------------------------------------
// Transitions & rejections
// ---------------------------------------------------------------------------

func TestPublishIdempotent(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()
	id := env.newPost(t, "Idempotent")

	if _, err := env.svc.PublishPost(ctx, id); err != nil {
		t.Fatalf("first publish error = %v", err)
	}
	before := env.row(t, id)

	// Publishing an already-published post: no-op, no second event.
	if _, err := env.svc.PublishPost(ctx, id); err != nil {
		t.Fatalf("second publish error = %v", err)
	}
	after := env.row(t, id)
	if !after.PublishedAt.Equal(*before.PublishedAt) {
		t.Fatalf("idempotent publish changed published_at: %v -> %v", before.PublishedAt, after.PublishedAt)
	}
	if len(env.events.published) != 1 {
		t.Fatalf("published events = %d, want 1 (no event on idempotent re-publish)", len(env.events.published))
	}
}

func TestUnpublishRules(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	// From draft: rejected with a status validation error.
	draft := env.newPost(t, "Draft")
	_, err := env.svc.UnpublishPost(ctx, draft)
	if !isFieldError(err, "status") {
		t.Fatalf("unpublish draft error = %v, want a status ValidationError", err)
	}

	// From scheduled: allowed, but not a "previously published" post, so no
	// PostUnpublished event.
	sched := env.newPost(t, "Scheduled")
	if _, err := env.svc.SchedulePost(ctx, sched, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("SchedulePost() error = %v", err)
	}
	if _, err := env.svc.UnpublishPost(ctx, sched); err != nil {
		t.Fatalf("unpublish scheduled error = %v", err)
	}
	if env.row(t, sched).Status != store.PostStatusDraft {
		t.Fatalf("unpublished scheduled status = %q, want draft", env.row(t, sched).Status)
	}
	if len(env.events.unpublished) != 0 {
		t.Fatalf("unpublished events = %d, want 0 (scheduled was never published)", len(env.events.unpublished))
	}

	// From published: allowed and fires PostUnpublished.
	pub := env.newPost(t, "Published")
	if _, err := env.svc.PublishPost(ctx, pub); err != nil {
		t.Fatalf("PublishPost() error = %v", err)
	}
	if _, err := env.svc.UnpublishPost(ctx, pub); err != nil {
		t.Fatalf("unpublish published error = %v", err)
	}
	if len(env.events.unpublished) != 1 || env.events.unpublished[0] != pub {
		t.Fatalf("unpublished events = %v, want [%s]", env.events.unpublished, pub)
	}
}

func TestScheduleRules(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	id := env.newPost(t, "Schedule")

	// Past time: rejected on scheduled_for.
	if _, err := env.svc.SchedulePost(ctx, id, time.Now().Add(-time.Minute)); !isFieldError(err, "scheduled_for") {
		t.Fatalf("schedule past error = %v, want a scheduled_for ValidationError", err)
	}

	// Future time: scheduled with scheduled_for set.
	when := time.Now().Add(2 * time.Hour)
	p, err := env.svc.SchedulePost(ctx, id, when)
	if err != nil {
		t.Fatalf("SchedulePost(future) error = %v", err)
	}
	if p.Row.Status != store.PostStatusScheduled || p.Row.ScheduledFor == nil {
		t.Fatalf("after schedule status=%q scheduledFor=%v, want scheduled + set", p.Row.Status, p.Row.ScheduledFor)
	}

	// From published: rejected on status.
	pub := env.newPost(t, "PublishedThenSchedule")
	if _, err := env.svc.PublishPost(ctx, pub); err != nil {
		t.Fatalf("PublishPost() error = %v", err)
	}
	if _, err := env.svc.SchedulePost(ctx, pub, time.Now().Add(time.Hour)); !isFieldError(err, "status") {
		t.Fatalf("schedule published error = %v, want a status ValidationError", err)
	}
}

func TestArchiveKeepsPublishedAt(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	id := env.newPost(t, "Archive Me")
	if _, err := env.svc.PublishPost(ctx, id); err != nil {
		t.Fatalf("PublishPost() error = %v", err)
	}
	published := env.row(t, id)

	p, err := env.svc.ArchivePost(ctx, id)
	if err != nil {
		t.Fatalf("ArchivePost() error = %v", err)
	}
	if p.Row.Status != store.PostStatusArchived {
		t.Fatalf("archive status = %q, want archived", p.Row.Status)
	}
	if p.Row.PublishedAt == nil || !p.Row.PublishedAt.Equal(*published.PublishedAt) {
		t.Fatalf("archive published_at = %v, want preserved %v", p.Row.PublishedAt, published.PublishedAt)
	}

	// Archiving a published post ends public visibility, so it fires exactly
	// one PostUnpublished.
	if len(env.events.unpublished) != 1 || env.events.unpublished[0] != id {
		t.Fatalf("unpublished events after archiving published = %v, want [%s]", env.events.unpublished, id)
	}

	// Idempotent from archived: no status change, no further event.
	if _, err := env.svc.ArchivePost(ctx, id); err != nil {
		t.Fatalf("re-archive error = %v", err)
	}
	if len(env.events.unpublished) != 1 {
		t.Fatalf("unpublished events after re-archive = %d, want still 1", len(env.events.unpublished))
	}
}

// TestArchiveDraftFiresNoUnpublished confirms archiving a non-published post
// (a draft here) does not fire PostUnpublished — it was never publicly live.
func TestArchiveDraftFiresNoUnpublished(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	id := env.newPost(t, "Draft To Archive")
	if _, err := env.svc.ArchivePost(ctx, id); err != nil {
		t.Fatalf("ArchivePost(draft) error = %v", err)
	}
	if len(env.events.unpublished) != 0 {
		t.Fatalf("unpublished events after archiving draft = %d, want 0", len(env.events.unpublished))
	}
}

// TestTransitionsUnknownPost confirms every operation 404s an unknown id.
func TestTransitionsUnknownPost(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()
	ops := map[string]func() error{
		"publish":   func() error { _, err := env.svc.PublishPost(ctx, "nope"); return err },
		"unpublish": func() error { _, err := env.svc.UnpublishPost(ctx, "nope"); return err },
		"schedule":  func() error { _, err := env.svc.SchedulePost(ctx, "nope", time.Now().Add(time.Hour)); return err },
		"archive":   func() error { _, err := env.svc.ArchivePost(ctx, "nope"); return err },
	}
	for name, op := range ops {
		if err := op(); err != store.ErrNotFound {
			t.Fatalf("%s unknown id error = %v, want store.ErrNotFound", name, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Update / delete events
// ---------------------------------------------------------------------------

func TestUpdateFiresOnlyWhenPublished(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()
	id := env.newPost(t, "Editable")

	// Update while draft: no PostUpdated.
	title := "Editable v2"
	if _, err := env.svc.UpdatePost(ctx, id, PostUpdateInput{Title: &title, CurrentUserID: env.authorID}); err != nil {
		t.Fatalf("UpdatePost(draft) error = %v", err)
	}
	if len(env.events.updated) != 0 {
		t.Fatalf("updated events after draft edit = %d, want 0", len(env.events.updated))
	}

	// Publish, then update: PostUpdated fires.
	if _, err := env.svc.PublishPost(ctx, id); err != nil {
		t.Fatalf("PublishPost() error = %v", err)
	}
	title = "Editable v3"
	if _, err := env.svc.UpdatePost(ctx, id, PostUpdateInput{Title: &title, CurrentUserID: env.authorID}); err != nil {
		t.Fatalf("UpdatePost(published) error = %v", err)
	}
	if len(env.events.updated) != 1 || env.events.updated[0] != id {
		t.Fatalf("updated events after published edit = %v, want [%s]", env.events.updated, id)
	}

	// A published post stays published after a normal update.
	if env.row(t, id).Status != store.PostStatusPublished {
		t.Fatalf("published post status after update = %q, want published", env.row(t, id).Status)
	}
}

func TestDeleteFiresOnlyWhenPublished(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()

	// Draft delete: no event.
	draft := env.newPost(t, "Draft Delete")
	if err := env.svc.DeletePost(ctx, draft); err != nil {
		t.Fatalf("DeletePost(draft) error = %v", err)
	}
	if len(env.events.deleted) != 0 {
		t.Fatalf("deleted events after draft delete = %d, want 0", len(env.events.deleted))
	}

	// Published delete: PostDeleted fires.
	pub := env.newPost(t, "Published Delete")
	if _, err := env.svc.PublishPost(ctx, pub); err != nil {
		t.Fatalf("PublishPost() error = %v", err)
	}
	if err := env.svc.DeletePost(ctx, pub); err != nil {
		t.Fatalf("DeletePost(published) error = %v", err)
	}
	if len(env.events.deleted) != 1 || env.events.deleted[0] != pub {
		t.Fatalf("deleted events = %v, want [%s]", env.events.deleted, pub)
	}
}

// ---------------------------------------------------------------------------
// Scheduler promotion
// ---------------------------------------------------------------------------

// TestPromoteScheduledPosts confirms overdue scheduled posts are promoted
// (published_at = scheduled_for, scheduled_for cleared, PostPublished fired),
// future ones are left alone, and a promoted post becomes publicly visible.
func TestPromoteScheduledPosts(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()
	// Stored timestamps are whole-second precision (RFC3339, no fractional
	// seconds), so seed with a truncated time to compare cleanly.
	now := time.Now().UTC().Truncate(time.Second)

	// Overdue scheduled post (seeded directly, since SchedulePost rejects a
	// past time).
	overdue := env.newPost(t, "Overdue")
	dueAt := now.Add(-10 * time.Minute)
	env.forceState(t, overdue, store.PostStatusScheduled, nil, &dueAt)

	// Future scheduled post: must NOT be promoted.
	future := env.newPost(t, "Future")
	if _, err := env.svc.SchedulePost(ctx, future, now.Add(time.Hour)); err != nil {
		t.Fatalf("SchedulePost(future) error = %v", err)
	}

	promoted, err := env.svc.PromoteScheduledPosts(ctx, now)
	if err != nil {
		t.Fatalf("PromoteScheduledPosts() error = %v", err)
	}
	if len(promoted) != 1 || promoted[0].Row.ID != overdue {
		t.Fatalf("promoted = %v, want just the overdue post", promoted)
	}

	got := env.row(t, overdue)
	if got.Status != store.PostStatusPublished {
		t.Fatalf("promoted status = %q, want published", got.Status)
	}
	if got.PublishedAt == nil || !got.PublishedAt.Equal(dueAt) {
		t.Fatalf("promoted published_at = %v, want scheduled_for %v", got.PublishedAt, dueAt)
	}
	if got.ScheduledFor != nil {
		t.Fatalf("promoted scheduled_for = %v, want cleared", got.ScheduledFor)
	}
	if len(env.events.published) != 1 || env.events.published[0] != overdue {
		t.Fatalf("published events = %v, want [%s]", env.events.published, overdue)
	}
	if env.row(t, future).Status != store.PostStatusScheduled {
		t.Fatalf("future post status = %q, want still scheduled", env.row(t, future).Status)
	}

	// The promoted post is now publicly visible.
	res, err := env.svc.ListPublicPosts(ctx, PublicPostListQuery{Page: 1, PerPage: 20, Sort: store.PostSortPublishedAt, SortDesc: true})
	if err != nil {
		t.Fatalf("ListPublicPosts() error = %v", err)
	}
	if len(res.Posts) != 1 || res.Posts[0].Row.ID != overdue {
		t.Fatalf("public list = %v, want just the promoted post", res.Posts)
	}
}

// TestPromoteKeepsExistingPublishedAt confirms a scheduled post that already
// carries a published_at (published, unpublished, then re-scheduled) keeps
// that original timestamp when promoted.
func TestPromoteKeepsExistingPublishedAt(t *testing.T) {
	env := newServiceEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	id := env.newPost(t, "Was Published")
	original := now.Add(-48 * time.Hour)
	dueAt := now.Add(-5 * time.Minute)
	// Seed: scheduled, but with an earlier published_at already recorded.
	env.forceState(t, id, store.PostStatusScheduled, &original, &dueAt)

	if _, err := env.svc.PromoteScheduledPosts(ctx, now); err != nil {
		t.Fatalf("PromoteScheduledPosts() error = %v", err)
	}
	got := env.row(t, id)
	if got.PublishedAt == nil || !got.PublishedAt.Equal(original) {
		t.Fatalf("promoted published_at = %v, want original %v (set-once)", got.PublishedAt, original)
	}
}

// isFieldError reports whether err is a *ValidationError carrying field.
func isFieldError(err error, field string) bool {
	ve, ok := err.(*ValidationError)
	if !ok {
		return false
	}
	_, has := ve.Fields[field]
	return has
}

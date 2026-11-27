package webhook

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// TestDispatchFanOut confirms a post event enqueues exactly one delivery per
// active, subscribed webhook: two subscribed actives get a row, an active that
// is not subscribed and a subscribed-but-inactive one get nothing.
func TestDispatchFanOut(t *testing.T) {
	db := newTestDB(t)
	dp := NewDispatcher(db, nil)

	subA := insertWebhook(t, db, "sub-a", "https://a.example.com", []string{EventPostPublished}, true)
	subB := insertWebhook(t, db, "sub-b", "https://b.example.com", []string{EventPostPublished, EventPostUpdated}, true)
	other := insertWebhook(t, db, "other", "https://c.example.com", []string{EventPostUpdated}, true)
	inactive := insertWebhook(t, db, "inactive", "https://d.example.com", []string{EventPostPublished}, false)

	publishedAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	p := &content.Post{Row: store.Post{
		ID:          "post-123",
		Slug:        "introducing-pocketpress",
		Status:      store.PostStatusPublished,
		PublishedAt: ptrTime(publishedAt),
	}}

	dp.PostPublished(context.Background(), p)

	if got := len(deliveries(t, db, subA.ID)); got != 1 {
		t.Fatalf("subscribed webhook A deliveries = %d, want 1", got)
	}
	if got := len(deliveries(t, db, subB.ID)); got != 1 {
		t.Fatalf("subscribed webhook B deliveries = %d, want 1", got)
	}
	if got := len(deliveries(t, db, other.ID)); got != 0 {
		t.Fatalf("unsubscribed webhook deliveries = %d, want 0", got)
	}
	if got := len(deliveries(t, db, inactive.ID)); got != 0 {
		t.Fatalf("inactive webhook deliveries = %d, want 0", got)
	}

	// Each row carries a distinct evt_ event_id and the frozen payload shape.
	dA := deliveries(t, db, subA.ID)[0]
	dB := deliveries(t, db, subB.ID)[0]
	if dA.EventID == dB.EventID {
		t.Fatalf("event_ids are equal (%q), want unique per row", dA.EventID)
	}
	for _, d := range []store.Delivery{dA, dB} {
		if d.Event != EventPostPublished || d.Status != store.DeliveryStatusPending || d.Attempt != 0 {
			t.Fatalf("delivery = {event:%q status:%q attempt:%d}, want {post.published pending 0}", d.Event, d.Status, d.Attempt)
		}
		if d.NextAttemptAt == nil {
			t.Fatal("delivery next_attempt_at = nil, want due-now")
		}
		var env struct {
			ID    string `json:"id"`
			Event string `json:"event"`
			Data  struct {
				Post struct {
					ID          string     `json:"id"`
					Slug        string     `json:"slug"`
					Status      string     `json:"status"`
					PublishedAt *time.Time `json:"published_at"`
				} `json:"post"`
			} `json:"data"`
		}
		if err := json.Unmarshal([]byte(d.Payload), &env); err != nil {
			t.Fatalf("payload not valid JSON: %v", err)
		}
		if env.ID != d.EventID {
			t.Fatalf("payload id = %q, want event_id %q", env.ID, d.EventID)
		}
		if env.Data.Post.ID != "post-123" || env.Data.Post.Slug != "introducing-pocketpress" || env.Data.Post.Status != "published" {
			t.Fatalf("payload post = %+v, want the published post", env.Data.Post)
		}
		if env.Data.Post.PublishedAt == nil || !env.Data.Post.PublishedAt.Equal(publishedAt) {
			t.Fatalf("payload published_at = %v, want %v", env.Data.Post.PublishedAt, publishedAt)
		}
	}
}

// TestDispatchRespectsFilters confirms category/tag filters skip non-matching
// posts while still delivering to unfiltered and matching webhooks.
func TestDispatchRespectsFilters(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()
	dp := NewDispatcher(db, nil)

	cat, err := store.CreateCategory(ctx, db, store.NewCategory{Name: "News", Slug: "news"})
	if err != nil {
		t.Fatalf("CreateCategory() error = %v", err)
	}
	tag, err := store.CreateTag(ctx, db, store.NewTag{Name: "Release", Slug: "release"})
	if err != nil {
		t.Fatalf("CreateTag() error = %v", err)
	}

	all := insertWebhook(t, db, "all", "https://all.example.com", []string{EventPostPublished}, true)
	catOnly := insertWebhookWithFilters(t, db, "cat", "https://cat.example.com",
		[]string{EventPostPublished}, true, store.WebhookFilters{CategoryIDs: []string{cat.ID}})
	tagOnly := insertWebhookWithFilters(t, db, "tag", "https://tag.example.com",
		[]string{EventPostPublished}, true, store.WebhookFilters{TagIDs: []string{tag.ID}})
	both := insertWebhookWithFilters(t, db, "both", "https://both.example.com",
		[]string{EventPostPublished}, true, store.WebhookFilters{
			CategoryIDs: []string{cat.ID}, TagIDs: []string{tag.ID},
		})
	otherCat := insertWebhookWithFilters(t, db, "other", "https://other.example.com",
		[]string{EventPostPublished}, true, store.WebhookFilters{CategoryIDs: []string{"missing-id"}})

	match := &content.Post{
		Row:        store.Post{ID: "p-match", Slug: "match", Status: store.PostStatusPublished},
		Categories: []store.Category{cat},
		Tags:       []store.Tag{tag},
	}
	dp.PostPublished(ctx, match)

	assertDeliveryCount := func(name, id string, want int) {
		t.Helper()
		if got := len(deliveries(t, db, id)); got != want {
			t.Fatalf("%s deliveries = %d, want %d", name, got, want)
		}
	}
	assertDeliveryCount("all", all.ID, 1)
	assertDeliveryCount("cat", catOnly.ID, 1)
	assertDeliveryCount("tag", tagOnly.ID, 1)
	assertDeliveryCount("both", both.ID, 1)
	assertDeliveryCount("other", otherCat.ID, 0)

	// Category-only post: tag filter and AND filter must skip.
	catPost := &content.Post{
		Row:        store.Post{ID: "p-cat", Slug: "cat-only", Status: store.PostStatusPublished},
		Categories: []store.Category{cat},
	}
	dp.PostPublished(ctx, catPost)
	assertDeliveryCount("all after cat-only", all.ID, 2)
	assertDeliveryCount("cat after cat-only", catOnly.ID, 2)
	assertDeliveryCount("tag after cat-only", tagOnly.ID, 1)
	assertDeliveryCount("both after cat-only", both.ID, 1)
}

// TestEnqueueTestIgnoresFilters confirms Send Test bypasses content filters
// the same way it bypasses event subscription.
func TestEnqueueTestIgnoresFilters(t *testing.T) {
	db := newTestDB(t)
	wh := insertWebhookWithFilters(t, db, "filtered", "https://x.example.com", nil, true,
		store.WebhookFilters{CategoryIDs: []string{"cat-never"}})
	id, err := NewDispatcher(db, nil).EnqueueTest(context.Background(), wh.ID)
	if err != nil {
		t.Fatalf("EnqueueTest() error = %v", err)
	}
	if getDelivery(t, db, id).Event != EventTest {
		t.Fatalf("expected webhook.test despite filters")
	}
}

// TestEnqueueTestIgnoresSubscription confirms a test delivery is enqueued
// regardless of the webhook's event subscription, with the webhook.test event
// and its frozen data shape.
func TestEnqueueTestIgnoresSubscription(t *testing.T) {
	db := newTestDB(t)
	dp := NewDispatcher(db, nil)
	// Subscribed to nothing — a test must still enqueue.
	wh := insertWebhook(t, db, "no-subs", "https://x.example.com", nil, true)

	id, err := dp.EnqueueTest(context.Background(), wh.ID)
	if err != nil {
		t.Fatalf("EnqueueTest() error = %v", err)
	}
	d := getDelivery(t, db, id)
	if d.Event != EventTest || d.Status != store.DeliveryStatusPending {
		t.Fatalf("test delivery = {event:%q status:%q}, want {webhook.test pending}", d.Event, d.Status)
	}

	var env struct {
		Event string `json:"event"`
		Data  struct {
			Webhook struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"webhook"`
			Message string `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(d.Payload), &env); err != nil {
		t.Fatalf("payload not valid JSON: %v", err)
	}
	if env.Data.Webhook.ID != wh.ID || env.Data.Webhook.Name != "no-subs" || env.Data.Message == "" {
		t.Fatalf("test payload data = %+v, want the webhook ref + a message", env.Data)
	}
}

// TestEnqueueTestUnknownWebhook confirms a test for a missing webhook surfaces
// store.ErrNotFound (mapped to 404 by the admin API).
func TestEnqueueTestUnknownWebhook(t *testing.T) {
	db := newTestDB(t)
	dp := NewDispatcher(db, nil)
	if _, err := dp.EnqueueTest(context.Background(), "nope"); err != store.ErrNotFound {
		t.Fatalf("EnqueueTest(unknown) error = %v, want store.ErrNotFound", err)
	}
}

// TestSchedulerPromotionEnqueuesDeliveries wires a content.Service to the
// dispatcher (as the router does) and confirms a scheduled-post promotion — the
// path that used to run through a second, noop-events service — now fans a
// post.published event out into delivery rows.
func TestSchedulerPromotionEnqueuesDeliveries(t *testing.T) {
	db := newTestDB(t)
	ctx := context.Background()

	author, err := store.CreateUser(ctx, db, store.NewUser{
		Email: "a@example.com", PasswordHash: "x", Name: "A", Role: store.RoleOwner,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	svc := content.NewService(db, nil)
	svc.SetEvents(NewDispatcher(db, nil))

	wh := insertWebhook(t, db, "rebuild", "https://x.example.com", []string{EventPostPublished}, true)

	// Seed an overdue scheduled post directly (SchedulePost rejects past times).
	post, err := svc.CreatePost(ctx, content.PostCreateInput{Title: "Due", CurrentUserID: author.ID})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}
	dueAt := time.Now().UTC().Add(-time.Minute)
	if err := store.SetPostStatus(ctx, db, post.Row.ID, store.PostStatusScheduled, nil, &dueAt); err != nil {
		t.Fatalf("SetPostStatus() error = %v", err)
	}

	if _, err := svc.PromoteScheduledPosts(ctx, time.Now().UTC()); err != nil {
		t.Fatalf("PromoteScheduledPosts() error = %v", err)
	}

	rows := deliveries(t, db, wh.ID)
	if len(rows) != 1 {
		t.Fatalf("deliveries after promotion = %d, want 1 (shared service must dispatch)", len(rows))
	}
	if rows[0].Event != EventPostPublished {
		t.Fatalf("delivery event = %q, want post.published", rows[0].Event)
	}
}

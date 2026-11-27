package http

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// createWebhookViaAPI POSTs a webhook as the authenticated owner and returns
// the created id and the issued secret.
func (e *testEnv) createWebhookViaAPI(t *testing.T, body map[string]any) (id, secret string) {
	t.Helper()
	resp := e.do(t, http.MethodPost, "/api/admin/v1/webhooks", body)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create webhook status = %d, want 201; body = %s", resp.StatusCode, raw)
	}
	wh, ok := m["webhook"].(map[string]any)
	if !ok {
		t.Fatalf("create webhook body has no webhook object: %s", raw)
	}
	id, _ = wh["id"].(string)
	secret, _ = m["secret"].(string)
	if id == "" || secret == "" {
		t.Fatalf("create webhook missing id/secret: %s", raw)
	}
	return id, secret
}

// TestWebhookCRUDAndSecretOnce covers the full webhook lifecycle and confirms
// the signing secret is only ever returned by create and rotate-secret — never
// by list or update.
func TestWebhookCRUDAndSecretOnce(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	id, secret := env.createWebhookViaAPI(t, map[string]any{
		"name":   "Rebuild site",
		"url":    "https://example.com/api/pocketpress-webhook",
		"events": []string{"post.published", "post.updated"},
	})
	if !strings.HasPrefix(secret, "whsec_") {
		t.Fatalf("secret = %q, want whsec_ prefix", secret)
	}

	// List: the webhook is present, active defaulted true, and the secret is
	// nowhere in the response.
	resp := env.do(t, http.MethodGet, "/api/admin/v1/webhooks", nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", resp.StatusCode)
	}
	if strings.Contains(string(raw), "whsec_") || strings.Contains(string(raw), "secret") {
		t.Fatalf("list leaked secret material: %s", raw)
	}
	list, _ := m["webhooks"].([]any)
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}
	first := list[0].(map[string]any)
	if first["active"] != true {
		t.Fatalf("active = %v, want true (defaulted)", first["active"])
	}

	// Update: change name, events and active; still no secret in the response.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/webhooks/"+id, map[string]any{
		"name":   "Rebuild (staging)",
		"events": []string{"post.deleted"},
		"active": false,
	})
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body = %s", resp.StatusCode, raw)
	}
	if strings.Contains(string(raw), "whsec_") {
		t.Fatalf("update leaked secret material: %s", raw)
	}
	wh := m["webhook"].(map[string]any)
	if wh["name"] != "Rebuild (staging)" || wh["active"] != false {
		t.Fatalf("update webhook = %v, want renamed + inactive", wh)
	}
	events, _ := wh["events"].([]any)
	if len(events) != 1 || events[0] != "post.deleted" {
		t.Fatalf("update events = %v, want [post.deleted]", events)
	}

	// Rotate: a fresh secret, different from the first, shown once.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/webhooks/"+id+"/rotate-secret", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("rotate status = %d, want 200", resp.StatusCode)
	}
	rotated, _ := m["secret"].(string)
	if !strings.HasPrefix(rotated, "whsec_") || rotated == secret {
		t.Fatalf("rotated secret = %q, want a new whsec_ value", rotated)
	}

	// Delete: 204, then the list is empty.
	resp = env.do(t, http.MethodDelete, "/api/admin/v1/webhooks/"+id, nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	resp = env.do(t, http.MethodGet, "/api/admin/v1/webhooks", nil)
	_, m = readJSON(t, resp)
	if list, _ := m["webhooks"].([]any); len(list) != 0 {
		t.Fatalf("list after delete len = %d, want 0", len(list))
	}
}

// TestWebhookFiltersCRUD confirms category/tag filters round-trip on create
// and patch, reject unknown ids, and default to empty arrays when omitted.
func TestWebhookFiltersCRUD(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodPost, "/api/admin/v1/categories", map[string]any{
		"name": "Changelog", "slug": "changelog",
	})
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create category status = %d; body = %s", resp.StatusCode, raw)
	}
	catID, _ := m["category"].(map[string]any)["id"].(string)

	resp = env.do(t, http.MethodPost, "/api/admin/v1/tags", map[string]any{
		"name": "Release", "slug": "release",
	})
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create tag status = %d; body = %s", resp.StatusCode, raw)
	}
	tagID, _ := m["tag"].(map[string]any)["id"].(string)

	id, _ := env.createWebhookViaAPI(t, map[string]any{
		"name":   "Filtered",
		"url":    "https://example.com/hook",
		"events": []string{"post.published"},
		"filters": map[string]any{
			"category_ids": []string{catID},
			"tag_ids":      []string{tagID},
		},
	})

	resp = env.do(t, http.MethodGet, "/api/admin/v1/webhooks", nil)
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d; body = %s", resp.StatusCode, raw)
	}
	list := m["webhooks"].([]any)
	wh := list[0].(map[string]any)
	filters := wh["filters"].(map[string]any)
	cats := filters["category_ids"].([]any)
	tags := filters["tag_ids"].([]any)
	if len(cats) != 1 || cats[0] != catID || len(tags) != 1 || tags[0] != tagID {
		t.Fatalf("filters = %#v, want category=%s tag=%s", filters, catID, tagID)
	}

	// Clear filters via PATCH.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/webhooks/"+id, map[string]any{
		"filters": map[string]any{"category_ids": []string{}, "tag_ids": []string{}},
	})
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear filters status = %d; body = %s", resp.StatusCode, raw)
	}
	filters = m["webhook"].(map[string]any)["filters"].(map[string]any)
	if len(filters["category_ids"].([]any)) != 0 || len(filters["tag_ids"].([]any)) != 0 {
		t.Fatalf("cleared filters = %#v, want empty arrays", filters)
	}

	// Unknown category id → validation_failed.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/webhooks/"+id, map[string]any{
		"filters": map[string]any{"category_ids": []string{"nope"}, "tag_ids": []string{}},
	})
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("unknown category = %d %s; body = %s", resp.StatusCode, errorCode(t, m), raw)
	}
	details := m["error"].(map[string]any)["details"].(map[string]any)
	if _, ok := details["category_ids"]; !ok {
		t.Fatalf("details missing category_ids: %v", details)
	}

	// Omitted filters on create → empty arrays in response.
	id2, _ := env.createWebhookViaAPI(t, map[string]any{
		"name": "Unfiltered", "url": "https://example.com/u", "events": []string{"post.updated"},
	})
	resp = env.do(t, http.MethodGet, "/api/admin/v1/webhooks", nil)
	_, m = readJSON(t, resp)
	foundDefault := false
	for _, item := range m["webhooks"].([]any) {
		row := item.(map[string]any)
		if row["id"] != id2 {
			continue
		}
		foundDefault = true
		f := row["filters"].(map[string]any)
		if len(f["category_ids"].([]any)) != 0 || len(f["tag_ids"].([]any)) != 0 {
			t.Fatalf("default filters = %#v, want empty", f)
		}
	}
	if !foundDefault {
		t.Fatalf("webhook %s not found in list", id2)
	}
}

// TestWebhookCreateValidation covers the field-level 400 envelope for the
// frozen payload fields name/url/events.
func TestWebhookCreateValidation(t *testing.T) {
	cases := []struct {
		name      string
		body      map[string]any
		wantField string
	}{
		{"missing name", map[string]any{"url": "https://example.com/h", "events": []string{"post.published"}}, "name"},
		{"bad url", map[string]any{"name": "X", "url": "not-a-url", "events": []string{"post.published"}}, "url"},
		{"unknown event", map[string]any{"name": "X", "url": "https://example.com/h", "events": []string{"post.exploded"}}, "events"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := newTestEnv(t, false)
			env.setup(t)

			resp := env.do(t, http.MethodPost, "/api/admin/v1/webhooks", tc.body)
			raw, m := readJSON(t, resp)
			if resp.StatusCode != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400; body = %s", resp.StatusCode, raw)
			}
			if errorCode(t, m) != "validation_failed" {
				t.Fatalf("code = %q, want validation_failed", errorCode(t, m))
			}
			details, ok := m["error"].(map[string]any)["details"].(map[string]any)
			if !ok {
				t.Fatalf("details is not an object: %s", raw)
			}
			if _, ok := details[tc.wantField]; !ok {
				t.Fatalf("details missing field %q: %v", tc.wantField, details)
			}
		})
	}
}

// TestWebhookPermissions confirms webhooks are owner-only: an editor is
// forbidden (403) and an anonymous client is unauthorized (401) on every
// webhook and delivery endpoint.
func TestWebhookPermissions(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	// Create an editor and log a second "browser" in as them.
	resp := env.do(t, http.MethodPost, "/api/admin/v1/users", map[string]any{
		"name": "Editor", "email": "editor@example.com", "password": "editor-strong-pass", "role": "editor",
	})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create editor status = %d, want 201", resp.StatusCode)
	}
	_ = resp.Body.Close()

	jar, _ := cookiejar.New(nil)
	editor := &http.Client{Jar: jar}
	resp = env.doWith(t, editor, http.MethodPost, "/api/admin/v1/auth/login", map[string]any{
		"email": "editor@example.com", "password": "editor-strong-pass",
	})
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("editor login status = %d, want 200", resp.StatusCode)
	}
	_ = resp.Body.Close()

	endpoints := []struct {
		method, path string
	}{
		{http.MethodGet, "/api/admin/v1/webhooks"},
		{http.MethodPost, "/api/admin/v1/webhooks"},
		{http.MethodPatch, "/api/admin/v1/webhooks/some-id"},
		{http.MethodDelete, "/api/admin/v1/webhooks/some-id"},
		{http.MethodPost, "/api/admin/v1/webhooks/some-id/rotate-secret"},
		{http.MethodPost, "/api/admin/v1/webhooks/some-id/test"},
		{http.MethodGet, "/api/admin/v1/webhooks/some-id/deliveries"},
		{http.MethodPost, "/api/admin/v1/deliveries/some-id/retry"},
	}

	for _, ep := range endpoints {
		// Editor → 403.
		resp := env.doWith(t, editor, ep.method, ep.path, nil)
		_, m := readJSON(t, resp)
		if resp.StatusCode != http.StatusForbidden || errorCode(t, m) != "forbidden" {
			t.Fatalf("%s %s as editor = %d %s, want 403 forbidden", ep.method, ep.path, resp.StatusCode, errorCode(t, m))
		}
		// Anonymous → 401.
		resp = env.doWith(t, &http.Client{}, ep.method, ep.path, nil)
		_, m = readJSON(t, resp)
		if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
			t.Fatalf("%s %s anon = %d %s, want 401 unauthorized", ep.method, ep.path, resp.StatusCode, errorCode(t, m))
		}
	}
}

// TestWebhookTestAndDeliveries confirms the "send test" endpoint enqueues a
// delivery (202 + delivery_id) that appears in the delivery history.
func TestWebhookTestAndDeliveries(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	id, _ := env.createWebhookViaAPI(t, map[string]any{
		"name":   "Tester",
		"url":    "https://example.com/hook",
		"events": []string{"post.published"},
	})

	resp := env.do(t, http.MethodPost, "/api/admin/v1/webhooks/"+id+"/test", nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("test status = %d, want 202; body = %s", resp.StatusCode, raw)
	}
	deliveryID, _ := m["delivery_id"].(string)
	if deliveryID == "" {
		t.Fatalf("test response missing delivery_id: %s", raw)
	}

	resp = env.do(t, http.MethodGet, "/api/admin/v1/webhooks/"+id+"/deliveries", nil)
	raw, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("deliveries status = %d, want 200", resp.StatusCode)
	}
	list, _ := m["deliveries"].([]any)
	if len(list) != 1 {
		t.Fatalf("deliveries len = %d, want 1; body = %s", len(list), raw)
	}
	d := list[0].(map[string]any)
	if d["event"] != "webhook.test" || d["status"] != "pending" || d["id"] != deliveryID {
		t.Fatalf("delivery = %v, want the pending webhook.test row %s", d, deliveryID)
	}
	if _, ok := m["meta"].(map[string]any); !ok {
		t.Fatalf("deliveries response missing meta: %s", raw)
	}

	// Deliveries for an unknown webhook → 404.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/webhooks/does-not-exist/deliveries", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "not_found" {
		t.Fatalf("deliveries unknown webhook = %d %s, want 404 not_found", resp.StatusCode, errorCode(t, m))
	}
}

// TestDeliveryRetryEndpoint confirms the manual retry endpoint: 202 for a
// failed delivery, 400 for a non-failed one, 404 for an unknown id.
func TestDeliveryRetryEndpoint(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	ctx := context.Background()

	id, _ := env.createWebhookViaAPI(t, map[string]any{
		"name":   "Retry target",
		"url":    "https://example.com/hook",
		"events": []string{"post.published"},
	})

	// A failed delivery (seeded directly) can be retried → 202.
	failed, err := store.CreateDelivery(ctx, env.db, store.NewDelivery{
		WebhookID: id, Event: "post.published", EventID: "evt_failed", Payload: "{}",
		Attempt: 5, Status: store.DeliveryStatusFailed,
	})
	if err != nil {
		t.Fatalf("seed failed delivery error = %v", err)
	}
	resp := env.do(t, http.MethodPost, "/api/admin/v1/deliveries/"+failed.ID+"/retry", nil)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("retry failed delivery status = %d, want 202", resp.StatusCode)
	}
	_ = resp.Body.Close()
	if d, _ := store.GetDeliveryByID(ctx, env.db, failed.ID); d.Status != store.DeliveryStatusPending {
		t.Fatalf("retried delivery status = %q, want pending", d.Status)
	}

	// A pending delivery cannot be retried → 400 invalid_state.
	now := time.Now().UTC()
	pending, err := store.CreateDelivery(ctx, env.db, store.NewDelivery{
		WebhookID: id, Event: "post.published", EventID: "evt_pending", Payload: "{}",
		Attempt: 0, Status: store.DeliveryStatusPending, NextAttemptAt: &now,
	})
	if err != nil {
		t.Fatalf("seed pending delivery error = %v", err)
	}
	resp = env.do(t, http.MethodPost, "/api/admin/v1/deliveries/"+pending.ID+"/retry", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "invalid_state" {
		t.Fatalf("retry pending delivery = %d %s, want 400 invalid_state", resp.StatusCode, errorCode(t, m))
	}

	// Unknown delivery → 404.
	resp = env.do(t, http.MethodPost, "/api/admin/v1/deliveries/does-not-exist/retry", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "not_found" {
		t.Fatalf("retry unknown delivery = %d %s, want 404 not_found", resp.StatusCode, errorCode(t, m))
	}
}

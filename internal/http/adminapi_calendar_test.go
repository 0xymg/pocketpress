package http

import (
	"net/http"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

func TestCalendarEndpoint(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	// Scheduled next week.
	sched := env.createPost(t, map[string]any{"title": "Queued", "slug": "queued"})
	when := now.Add(7 * 24 * time.Hour).Truncate(time.Second)
	env.mustAction(t, sched["id"].(string), "schedule", map[string]any{
		"scheduled_for": when.Format(time.RFC3339),
	})

	// Published yesterday.
	pub := env.createPost(t, map[string]any{"title": "Shipped", "slug": "shipped"})
	pubAt := now.Add(-24 * time.Hour).Truncate(time.Second)
	env.publishPost(t, pub["id"].(string), pubAt)

	// Draft — must not appear.
	env.createPost(t, map[string]any{"title": "WIP", "slug": "wip"})

	from := now.Add(-48 * time.Hour).Format("2006-01-02")
	to := now.Add(14 * 24 * time.Hour).Format("2006-01-02")

	resp := env.do(t, http.MethodGet, "/api/admin/v1/calendar?from="+from+"&to="+to, nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /calendar status = %d body=%s, want 200", resp.StatusCode, raw)
	}
	entries, _ := m["entries"].([]any)
	if len(entries) != 2 {
		t.Fatalf("entries len = %d, want 2; body=%s", len(entries), raw)
	}

	ids := map[string]string{}
	for _, e := range entries {
		row := e.(map[string]any)
		ids[row["id"].(string)] = row["status"].(string)
		if row["at"] == nil || row["at"] == "" {
			t.Fatalf("entry missing at: %v", row)
		}
		if row["author"] == nil {
			t.Fatalf("entry missing author: %v", row)
		}
	}
	if ids[sched["id"].(string)] != store.PostStatusScheduled {
		t.Fatalf("scheduled status = %q", ids[sched["id"].(string)])
	}
	if ids[pub["id"].(string)] != store.PostStatusPublished {
		t.Fatalf("published status = %q", ids[pub["id"].(string)])
	}

	// Auth required.
	anon := &http.Client{}
	resp = env.doWith(t, anon, http.MethodGet, "/api/admin/v1/calendar?from="+from+"&to="+to, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anon GET /calendar = %d, want 401", resp.StatusCode)
	}

	// Missing bounds → 400.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/calendar", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing bounds = %d, want 400", resp.StatusCode)
	}
	details := validationDetails(t, m)
	if _, ok := details["from"]; !ok {
		t.Fatalf("missing from detail: %v", m)
	}
	if _, ok := details["to"]; !ok {
		t.Fatalf("missing to detail: %v", m)
	}

	// Inverted range → 400.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/calendar?from=2026-12-01&to=2026-11-01", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("inverted = %d, want 400", resp.StatusCode)
	}
	if _, ok := validationDetails(t, m)["to"]; !ok {
		t.Fatalf("inverted missing to detail: %v", m)
	}

	// Too wide → 400.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/calendar?from=2026-01-01&to=2026-06-01", nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("too wide = %d, want 400", resp.StatusCode)
	}
}

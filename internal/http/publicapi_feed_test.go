package http

import (
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestPublicFeedRSSAndAtom covers the v0.2 syndication endpoints:
// Content-Type, Cache-Control, newest-published ordering, draft exclusion,
// canonical_url vs site_url/slug permalinks, and empty-feed shape.
func TestPublicFeedRSSAndAtom(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	older := env.createPost(t, map[string]any{
		"title": "Older Post", "slug": "older-post",
		"excerpt": "Older excerpt.", "content_html": "<p>Older body</p>",
	})
	env.publishPost(t, older["id"].(string), now.Add(-2*time.Hour))

	newer := env.createPost(t, map[string]any{
		"title": "Newer Post", "slug": "newer-post",
		"excerpt": "Newer excerpt.", "content_html": "<p>Newer body</p>",
		"canonical_url": "https://example.com/blog/newer-post",
	})
	env.publishPost(t, newer["id"].(string), now.Add(-time.Hour))

	draft := env.createPost(t, map[string]any{
		"title": "Secret Draft", "slug": "secret-draft",
		"excerpt": "Should not appear.",
	})
	_ = draft

	// --- RSS ----------------------------------------------------------------
	resp := env.do(t, http.MethodGet, "/api/v1/feed.rss", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /feed.rss status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/rss+xml; charset=utf-8" {
		t.Fatalf("GET /feed.rss Content-Type = %q, want application/rss+xml; charset=utf-8", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=60" {
		t.Fatalf("GET /feed.rss Cache-Control = %q, want public, max-age=60", cc)
	}
	if nosniff := resp.Header.Get("X-Content-Type-Options"); nosniff != "nosniff" {
		t.Fatalf("GET /feed.rss X-Content-Type-Options = %q, want nosniff", nosniff)
	}

	rssBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read RSS body: %v", err)
	}
	rssRaw := string(rssBody)
	if !strings.HasPrefix(rssRaw, xml.Header) && !strings.HasPrefix(rssRaw, "<?xml") {
		t.Fatalf("RSS body missing XML declaration: %q", rssRaw[:min(80, len(rssRaw))])
	}

	var rss struct {
		Channel struct {
			Title string `xml:"title"`
			Link  string `xml:"link"`
			Items []struct {
				Title string `xml:"title"`
				Link  string `xml:"link"`
				GUID  struct {
					IsPermaLink string `xml:"isPermaLink,attr"`
					Value       string `xml:",chardata"`
				} `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(rssBody, &rss); err != nil {
		t.Fatalf("unmarshal RSS: %v\nbody=%s", err, rssRaw)
	}
	if rss.Channel.Title != "My Blog" {
		t.Fatalf("RSS channel title = %q, want My Blog", rss.Channel.Title)
	}
	if rss.Channel.Link != "https://example.com" {
		t.Fatalf("RSS channel link = %q, want https://example.com", rss.Channel.Link)
	}
	if len(rss.Channel.Items) != 2 {
		t.Fatalf("RSS items = %d, want 2 (draft excluded)", len(rss.Channel.Items))
	}
	if rss.Channel.Items[0].Title != "Newer Post" || rss.Channel.Items[1].Title != "Older Post" {
		t.Fatalf("RSS order = %q, %q; want Newer then Older",
			rss.Channel.Items[0].Title, rss.Channel.Items[1].Title)
	}
	if rss.Channel.Items[0].Link != "https://example.com/blog/newer-post" {
		t.Fatalf("RSS newer link = %q, want canonical_url", rss.Channel.Items[0].Link)
	}
	if rss.Channel.Items[1].Link != "https://example.com/older-post" {
		t.Fatalf("RSS older link = %q, want site_url/slug fallback", rss.Channel.Items[1].Link)
	}
	if rss.Channel.Items[0].GUID.IsPermaLink != "false" || !strings.HasPrefix(rss.Channel.Items[0].GUID.Value, "urn:uuid:") {
		t.Fatalf("RSS guid = %+v, want isPermaLink=false urn:uuid:...", rss.Channel.Items[0].GUID)
	}
	if strings.Contains(rssRaw, "Secret Draft") {
		t.Fatal("RSS feed leaked a draft post")
	}

	// --- Atom ---------------------------------------------------------------
	resp = env.do(t, http.MethodGet, "/api/v1/feed.atom", nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /feed.atom status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/atom+xml; charset=utf-8" {
		t.Fatalf("GET /feed.atom Content-Type = %q, want application/atom+xml; charset=utf-8", ct)
	}

	atomBody, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read Atom body: %v", err)
	}
	atomRaw := string(atomBody)

	var atom struct {
		Title string `xml:"title"`
		ID    string `xml:"id"`
		Links []struct {
			Rel  string `xml:"rel,attr"`
			Href string `xml:"href,attr"`
		} `xml:"link"`
		Entries []struct {
			Title string `xml:"title"`
			ID    string `xml:"id"`
			Links []struct {
				Href string `xml:"href,attr"`
			} `xml:"link"`
			Author *struct {
				Name string `xml:"name"`
			} `xml:"author"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal(atomBody, &atom); err != nil {
		t.Fatalf("unmarshal Atom: %v\nbody=%s", err, atomRaw)
	}
	if atom.Title != "My Blog" {
		t.Fatalf("Atom title = %q, want My Blog", atom.Title)
	}
	if atom.ID != "https://example.com" {
		t.Fatalf("Atom feed id = %q, want https://example.com", atom.ID)
	}
	var selfHref string
	for _, l := range atom.Links {
		if l.Rel == "self" {
			selfHref = l.Href
		}
	}
	if !strings.HasSuffix(selfHref, "/api/v1/feed.atom") {
		t.Fatalf("Atom rel=self href = %q, want .../api/v1/feed.atom", selfHref)
	}
	if len(atom.Entries) != 2 {
		t.Fatalf("Atom entries = %d, want 2", len(atom.Entries))
	}
	if atom.Entries[0].Title != "Newer Post" {
		t.Fatalf("Atom first entry = %q, want Newer Post", atom.Entries[0].Title)
	}
	if atom.Entries[0].Links[0].Href != "https://example.com/blog/newer-post" {
		t.Fatalf("Atom newer link = %q, want canonical_url", atom.Entries[0].Links[0].Href)
	}
	if atom.Entries[0].Author == nil || atom.Entries[0].Author.Name == "" {
		t.Fatalf("Atom entry missing author name: %+v", atom.Entries[0].Author)
	}
	if !strings.HasPrefix(atom.Entries[0].ID, "urn:uuid:") {
		t.Fatalf("Atom entry id = %q, want urn:uuid:...", atom.Entries[0].ID)
	}
	if strings.Contains(atomRaw, "Secret Draft") {
		t.Fatal("Atom feed leaked a draft post")
	}
}

// TestPublicFeedEmpty confirms an instance with no published posts still
// returns a well-formed empty channel/feed.
func TestPublicFeedEmpty(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.do(t, http.MethodGet, "/api/v1/feed.rss", nil)
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty RSS status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var rss struct {
		Channel struct {
			Items []struct{} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal(body, &rss); err != nil {
		t.Fatalf("unmarshal empty RSS: %v", err)
	}
	if len(rss.Channel.Items) != 0 {
		t.Fatalf("empty RSS items = %d, want 0", len(rss.Channel.Items))
	}

	resp = env.do(t, http.MethodGet, "/api/v1/feed.atom", nil)
	body, err = io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty Atom status = %d, want 200; body=%s", resp.StatusCode, body)
	}
	var atom struct {
		Entries []struct{} `xml:"entry"`
	}
	if err := xml.Unmarshal(body, &atom); err != nil {
		t.Fatalf("unmarshal empty Atom: %v", err)
	}
	if len(atom.Entries) != 0 {
		t.Fatalf("empty Atom entries = %d, want 0", len(atom.Entries))
	}
}

// TestPublicFeedRespectsVisibility confirms scheduled-future and archived
// posts never appear in either feed.
func TestPublicFeedRespectsVisibility(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)
	now := time.Now().UTC()

	pub := env.createPost(t, map[string]any{"title": "Live", "slug": "live"})
	env.publishPost(t, pub["id"].(string), now.Add(-time.Minute))

	sched := env.createPost(t, map[string]any{"title": "Future", "slug": "future"})
	future := now.Add(24 * time.Hour)
	env.setPostState(t, sched["id"].(string), "scheduled", &future)

	arch := env.createPost(t, map[string]any{"title": "Archived", "slug": "archived"})
	env.setPostState(t, arch["id"].(string), "archived", &now)

	for _, path := range []string{"/api/v1/feed.rss", "/api/v1/feed.atom"} {
		resp := env.do(t, http.MethodGet, path, nil)
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		raw := string(body)
		if !strings.Contains(raw, "Live") {
			t.Fatalf("%s missing published post: %s", path, raw)
		}
		for _, leaked := range []string{"Future", "Archived"} {
			if strings.Contains(raw, leaked) {
				t.Fatalf("%s leaked %q: %s", path, leaked, raw)
			}
		}
	}
}

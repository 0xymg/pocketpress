package publicapi

import (
	"encoding/xml"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// feedMaxItems is how many newest published posts each feed returns. Feeds
// are intentionally uncapped by query params in v0.2 — keep them small and
// predictable like the public list default.
const feedMaxItems = 20

const (
	rssContentType  = "application/rss+xml; charset=utf-8"
	atomContentType = "application/atom+xml; charset=utf-8"
)

// feedRSS handles GET /api/v1/feed.rss — an RSS 2.0 document of the newest
// publicly-visible posts.
func (h *Handler) feedRSS(w http.ResponseWriter, r *http.Request) {
	h.writeFeed(w, r, false)
}

// feedAtom handles GET /api/v1/feed.atom — an Atom 1.0 document of the
// newest publicly-visible posts.
func (h *Handler) feedAtom(w http.ResponseWriter, r *http.Request) {
	h.writeFeed(w, r, true)
}

func (h *Handler) writeFeed(w http.ResponseWriter, r *http.Request, atom bool) {
	settings, err := store.GetSettings(r.Context(), h.db)
	if err != nil {
		// Gate already required a settings row when public_api_enabled is
		// true; a missing row here is an unexpected race/error.
		h.serverError(w, err)
		return
	}

	result, err := h.content.ListPublicPosts(r.Context(), content.PublicPostListQuery{
		Page:     1,
		PerPage:  feedMaxItems,
		Sort:     store.PostSortPublishedAt,
		SortDesc: true,
	})
	if err != nil {
		h.serverError(w, err)
		return
	}

	feedURL := absoluteRequestURL(r)
	updated := feedUpdatedAt(result.Posts)

	w.Header().Set("Cache-Control", resolveCacheControl(r.Context(), listCacheControl))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if atom {
		w.Header().Set("Content-Type", atomContentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(xml.Header))
		enc := xml.NewEncoder(w)
		enc.Indent("", "  ")
		_ = enc.Encode(buildAtomFeed(settings, feedURL, updated, result.Posts))
		_, _ = w.Write([]byte("\n"))
		return
	}

	w.Header().Set("Content-Type", rssContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(buildRSSFeed(settings, updated, result.Posts))
	_, _ = w.Write([]byte("\n"))
}

func feedUpdatedAt(posts []content.Post) time.Time {
	var latest time.Time
	for _, p := range posts {
		t := p.Row.UpdatedAt
		if p.Row.PublishedAt != nil && p.Row.PublishedAt.After(t) {
			t = *p.Row.PublishedAt
		}
		if t.After(latest) {
			latest = t
		}
	}
	if latest.IsZero() {
		return time.Now().UTC()
	}
	return latest.UTC()
}

// postPermalink prefers an explicit SEO canonical URL; otherwise joins
// settings.site_url with the post slug (headless frontends own their path
// layout — slug-at-root is the boring default documented for feeds).
func postPermalink(siteURL, slug, canonicalURL string) string {
	if strings.TrimSpace(canonicalURL) != "" {
		return strings.TrimSpace(canonicalURL)
	}
	base := strings.TrimRight(strings.TrimSpace(siteURL), "/")
	if base == "" {
		return "/" + slug
	}
	return base + "/" + slug
}

// absoluteRequestURL rebuilds the request's absolute URL for Atom's
// rel=self link. Prefer X-Forwarded-Proto when a reverse proxy terminates
// TLS in front of PocketPress.
func absoluteRequestURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "http" || proto == "https" {
		scheme = proto
	}
	u := &url.URL{
		Scheme: scheme,
		Host:   r.Host,
		Path:   r.URL.Path,
	}
	return u.String()
}

func postSummary(p content.Post) string {
	if strings.TrimSpace(p.Row.Excerpt) != "" {
		return p.Row.Excerpt
	}
	return p.Row.ContentText
}

// ---------------------------------------------------------------------------
// RSS 2.0
// ---------------------------------------------------------------------------

type rssRoot struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate"`
	Items         []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	PubDate     string  `xml:"pubDate"`
	Description string  `xml:"description"`
}

type rssGUID struct {
	IsPermaLink string `xml:"isPermaLink,attr"`
	Value       string `xml:",chardata"`
}

func buildRSSFeed(settings store.Settings, updated time.Time, posts []content.Post) rssRoot {
	items := make([]rssItem, 0, len(posts))
	for _, p := range posts {
		link := postPermalink(settings.SiteURL, p.Row.Slug, p.Row.CanonicalURL)
		pub := updated
		if p.Row.PublishedAt != nil {
			pub = p.Row.PublishedAt.UTC()
		}
		items = append(items, rssItem{
			Title: p.Row.Title,
			Link:  link,
			GUID: rssGUID{
				IsPermaLink: "false",
				Value:       "urn:uuid:" + p.Row.ID,
			},
			PubDate:     pub.Format(time.RFC1123Z),
			Description: postSummary(p),
		})
	}

	desc := "Posts from " + settings.SiteName
	return rssRoot{
		Version: "2.0",
		Channel: rssChannel{
			Title:         settings.SiteName,
			Link:          strings.TrimRight(settings.SiteURL, "/"),
			Description:   desc,
			LastBuildDate: updated.Format(time.RFC1123Z),
			Items:         items,
		},
	}
}

// ---------------------------------------------------------------------------
// Atom 1.0
// ---------------------------------------------------------------------------

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	XMLNS   string      `xml:"xmlns,attr"`
	Title   string      `xml:"title"`
	Links   []atomLink  `xml:"link"`
	ID      string      `xml:"id"`
	Updated string      `xml:"updated"`
	Entries []atomEntry `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr,omitempty"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr,omitempty"`
}

type atomEntry struct {
	Title     string       `xml:"title"`
	Links     []atomLink   `xml:"link"`
	ID        string       `xml:"id"`
	Published string       `xml:"published,omitempty"`
	Updated   string       `xml:"updated"`
	Author    *atomPerson  `xml:"author,omitempty"`
	Summary   string       `xml:"summary,omitempty"`
	Content   *atomContent `xml:"content,omitempty"`
}

type atomPerson struct {
	Name string `xml:"name"`
}

type atomContent struct {
	Type string `xml:"type,attr"`
	Body string `xml:",chardata"`
}

func buildAtomFeed(settings store.Settings, selfURL string, updated time.Time, posts []content.Post) atomFeed {
	siteLink := strings.TrimRight(settings.SiteURL, "/")
	entries := make([]atomEntry, 0, len(posts))
	for _, p := range posts {
		link := postPermalink(settings.SiteURL, p.Row.Slug, p.Row.CanonicalURL)
		pub := ""
		if p.Row.PublishedAt != nil {
			pub = p.Row.PublishedAt.UTC().Format(time.RFC3339)
		}
		entry := atomEntry{
			Title:     p.Row.Title,
			Links:     []atomLink{{Rel: "alternate", Href: link}},
			ID:        "urn:uuid:" + p.Row.ID,
			Published: pub,
			Updated:   p.Row.UpdatedAt.UTC().Format(time.RFC3339),
			Author:    &atomPerson{Name: p.Author.Name},
			Summary:   postSummary(p),
		}
		if p.Row.ContentHTML != "" {
			entry.Content = &atomContent{Type: "html", Body: p.Row.ContentHTML}
		}
		entries = append(entries, entry)
	}

	feedID := siteLink
	if feedID == "" {
		feedID = selfURL
	}

	return atomFeed{
		XMLNS: "http://www.w3.org/2005/Atom",
		Title: settings.SiteName,
		Links: []atomLink{
			{Rel: "alternate", Href: siteLink},
			{Rel: "self", Href: selfURL, Type: "application/atom+xml"},
		},
		ID:      feedID,
		Updated: updated.Format(time.RFC3339),
		Entries: entries,
	}
}

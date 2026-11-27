package publicapi

import (
	"encoding/xml"
	"net/http"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/store"
)

// sitemapMaxURLs is the Sitemap protocol cap (50,000 URLs per document).
// PocketPress is a tiny blog backend — one document is enough for v0.2.
const sitemapMaxURLs = 50000

// sitemapPageSize is how many public posts we pull per ListPublicPosts
// page while assembling the sitemap. Larger than the public JSON API's
// per_page cap because this path is server-driven, not client-paginated.
const sitemapPageSize = 500

const sitemapContentType = "application/xml; charset=utf-8"

// sitemap handles GET /api/v1/sitemap.xml — a Sitemap 0.9 document of
// publicly-visible posts (and the site homepage when site_url is set).
func (h *Handler) sitemap(w http.ResponseWriter, r *http.Request) {
	settings, err := store.GetSettings(r.Context(), h.db)
	if err != nil {
		h.serverError(w, err)
		return
	}

	posts, err := h.listAllPublicPosts(r)
	if err != nil {
		h.serverError(w, err)
		return
	}

	doc := buildSitemap(settings, posts)

	w.Header().Set("Cache-Control", listCacheControl)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", sitemapContentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(doc)
	_, _ = w.Write([]byte("\n"))
}

// listAllPublicPosts walks ListPublicPosts pages until every publicly-
// visible post is collected, or sitemapMaxURLs post entries would be
// exceeded (leaving room for an optional homepage entry).
func (h *Handler) listAllPublicPosts(r *http.Request) ([]content.Post, error) {
	var all []content.Post
	for page := 1; ; page++ {
		result, err := h.content.ListPublicPosts(r.Context(), content.PublicPostListQuery{
			Page:     page,
			PerPage:  sitemapPageSize,
			Sort:     store.PostSortPublishedAt,
			SortDesc: true,
		})
		if err != nil {
			return nil, err
		}
		all = append(all, result.Posts...)
		if len(all) >= sitemapMaxURLs {
			return all[:sitemapMaxURLs], nil
		}
		if page >= result.TotalPages || len(result.Posts) == 0 {
			return all, nil
		}
	}
}

// ---------------------------------------------------------------------------
// Sitemap 0.9 (http://www.sitemaps.org/schemas/sitemap/0.9)
// ---------------------------------------------------------------------------

type sitemapURLSet struct {
	XMLName xml.Name     `xml:"urlset"`
	XMLNS   string       `xml:"xmlns,attr"`
	URLs    []sitemapURL `xml:"url"`
}

type sitemapURL struct {
	Loc     string `xml:"loc"`
	LastMod string `xml:"lastmod,omitempty"`
}

func buildSitemap(settings store.Settings, posts []content.Post) sitemapURLSet {
	urls := make([]sitemapURL, 0, len(posts)+1)

	siteURL := strings.TrimRight(strings.TrimSpace(settings.SiteURL), "/")
	if siteURL != "" {
		urls = append(urls, sitemapURL{Loc: siteURL})
	}

	for _, p := range posts {
		if len(urls) >= sitemapMaxURLs {
			break
		}
		loc := postPermalink(settings.SiteURL, p.Row.Slug, p.Row.CanonicalURL)
		entry := sitemapURL{Loc: loc}
		if lm := sitemapLastMod(p); lm != "" {
			entry.LastMod = lm
		}
		urls = append(urls, entry)
	}

	return sitemapURLSet{
		XMLNS: "http://www.sitemaps.org/schemas/sitemap/0.9",
		URLs:  urls,
	}
}

func sitemapLastMod(p content.Post) string {
	t := p.Row.UpdatedAt
	if p.Row.PublishedAt != nil && p.Row.PublishedAt.After(t) {
		t = *p.Row.PublishedAt
	}
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

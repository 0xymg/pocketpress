// Package ghost parses Ghost content export JSON into importable posts.
//
// Supported shapes:
//   - Full export: {"db":[{"meta":...,"data":{...}}]}
//   - Bare data object: {"posts":[...],"tags":[...],"posts_tags":[...]}
//
// Only posts with type "post" (or missing type) are returned. Pages, emails
// and other content types are skipped. Tags are resolved via posts_tags.
package ghost

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// MaxExportBytes caps a single Ghost export JSON payload.
const MaxExportBytes = 20 << 20 // 20 MiB

// MaxPosts caps how many posts one export may contribute.
const MaxPosts = 200

// Document is one Ghost post ready for PocketPress import.
type Document struct {
	SourceKey      string // slug or ghost id — used in import result rows
	Title          string
	Slug           string
	Excerpt        string
	HTML           string
	LexicalJSON    string // optional; prefer when ValidLexicalJSON
	Status         string // draft | published
	PublishedAt    *time.Time
	Featured       bool
	Tags           []string
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
}

type exportRoot struct {
	DB []exportDB `json:"db"`
	// Bare data fields (when the file is just the data object).
	Posts     []ghostPost    `json:"posts"`
	Tags      []ghostTag     `json:"tags"`
	PostsTags []ghostPostTag `json:"posts_tags"`
}

type exportDB struct {
	Data exportData `json:"data"`
}

type exportData struct {
	Posts     []ghostPost    `json:"posts"`
	Tags      []ghostTag     `json:"tags"`
	PostsTags []ghostPostTag `json:"posts_tags"`
}

type ghostPost struct {
	ID              json.RawMessage `json:"id"`
	Title           string          `json:"title"`
	Slug            string          `json:"slug"`
	HTML            string          `json:"html"`
	Lexical         string          `json:"lexical"`
	Type            string          `json:"type"`
	Status          string          `json:"status"`
	Featured        boolish         `json:"featured"`
	CustomExcerpt   string          `json:"custom_excerpt"`
	PublishedAt     *flexibleTime   `json:"published_at"`
	MetaTitle       string          `json:"meta_title"`
	MetaDescription string          `json:"meta_description"`
	CanonicalURL    string          `json:"canonical_url"`
	OgTitle         string          `json:"og_title"`
	OgDescription   string          `json:"og_description"`
	FeatureImage    string          `json:"feature_image"` // ignored (no media download)
	Plaintext       string          `json:"plaintext"`
}

type ghostTag struct {
	ID   json.RawMessage `json:"id"`
	Name string          `json:"name"`
	Slug string          `json:"slug"`
}

type ghostPostTag struct {
	PostID json.RawMessage `json:"post_id"`
	TagID  json.RawMessage `json:"tag_id"`
}

// Parse decodes a Ghost export and returns importable post documents.
func Parse(raw []byte) ([]Document, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("Ghost export is empty")
	}
	if len(raw) > MaxExportBytes {
		return nil, fmt.Errorf("Ghost export exceeds %d bytes", MaxExportBytes)
	}

	var root exportRoot
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("Ghost export is not valid JSON")
	}

	var posts []ghostPost
	var tags []ghostTag
	var postsTags []ghostPostTag

	switch {
	case len(root.DB) > 0:
		data := root.DB[0].Data
		posts, tags, postsTags = data.Posts, data.Tags, data.PostsTags
	case len(root.Posts) > 0:
		posts, tags, postsTags = root.Posts, root.Tags, root.PostsTags
	default:
		return nil, fmt.Errorf("Ghost export has no posts")
	}

	tagByID := make(map[string]ghostTag, len(tags))
	for _, tg := range tags {
		id := rawID(tg.ID)
		if id == "" {
			continue
		}
		tagByID[id] = tg
	}
	tagsByPost := make(map[string][]string)
	for _, pt := range postsTags {
		pid := rawID(pt.PostID)
		tid := rawID(pt.TagID)
		if pid == "" || tid == "" {
			continue
		}
		tg, ok := tagByID[tid]
		if !ok {
			continue
		}
		name := strings.TrimSpace(tg.Name)
		if name == "" {
			name = strings.TrimSpace(tg.Slug)
		}
		if name == "" {
			continue
		}
		tagsByPost[pid] = append(tagsByPost[pid], name)
	}

	docs := make([]Document, 0, len(posts))
	for _, p := range posts {
		typ := strings.ToLower(strings.TrimSpace(p.Type))
		if typ != "" && typ != "post" {
			continue
		}
		doc, ok := mapPost(p, tagsByPost[rawID(p.ID)])
		if !ok {
			continue
		}
		docs = append(docs, doc)
		if len(docs) > MaxPosts {
			return nil, fmt.Errorf("Ghost export has more than %d posts", MaxPosts)
		}
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("Ghost export has no importable posts")
	}
	return docs, nil
}

func mapPost(p ghostPost, tagNames []string) (Document, bool) {
	status, wantPublish := mapStatus(p.Status)
	if status == "" {
		return Document{}, false // trash / unknown — skip
	}

	title := strings.TrimSpace(p.Title)
	slug := strings.TrimSpace(p.Slug)
	source := slug
	if source == "" {
		source = rawID(p.ID)
	}
	if source == "" {
		source = "untitled"
	}
	if title == "" {
		title = "Untitled"
	}

	excerpt := strings.TrimSpace(p.CustomExcerpt)
	seoTitle := firstNonEmpty(p.MetaTitle, p.OgTitle)
	seoDesc := firstNonEmpty(p.MetaDescription, p.OgDescription)

	var publishedAt *time.Time
	if wantPublish && p.PublishedAt != nil && !p.PublishedAt.Time.IsZero() {
		t := p.PublishedAt.Time.UTC()
		publishedAt = &t
	}

	return Document{
		SourceKey:      source,
		Title:          title,
		Slug:           slug,
		Excerpt:        excerpt,
		HTML:           p.HTML,
		LexicalJSON:    strings.TrimSpace(p.Lexical),
		Status:         status,
		PublishedAt:    publishedAt,
		Featured:       bool(p.Featured),
		Tags:           dedupeNames(tagNames),
		SEOTitle:       strings.TrimSpace(seoTitle),
		SEODescription: strings.TrimSpace(seoDesc),
		CanonicalURL:   strings.TrimSpace(p.CanonicalURL),
	}, true
}

func mapStatus(raw string) (status string, publish bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "draft":
		return "draft", false
	case "published":
		return "published", true
	case "scheduled", "sent":
		// Treat as draft — PocketPress import does not create scheduled posts.
		return "draft", false
	case "deleted", "trash":
		return "", false
	default:
		return "draft", false
	}
}

func rawID(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return strings.TrimSpace(s)
	}
	var n json.Number
	if err := json.Unmarshal(raw, &n); err == nil {
		return n.String()
	}
	return strings.Trim(string(raw), `"`)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func dedupeNames(names []string) []string {
	seen := make(map[string]struct{}, len(names))
	out := make([]string, 0, len(names))
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		key := strings.ToLower(n)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, n)
	}
	return out
}

// boolish accepts Ghost's featured as bool or 0/1.
type boolish bool

func (b *boolish) UnmarshalJSON(data []byte) error {
	s := strings.TrimSpace(string(data))
	switch s {
	case "true", "1", `"1"`, `"true"`:
		*b = true
	default:
		*b = false
	}
	return nil
}

// flexibleTime parses Ghost timestamps (RFC3339 / MySQL-ish).
type flexibleTime struct {
	Time time.Time
}

func (t *flexibleTime) UnmarshalJSON(data []byte) error {
	s := strings.Trim(string(data), `"`)
	if s == "" || s == "null" {
		return nil
	}
	formats := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.000Z",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
	}
	for _, f := range formats {
		if parsed, err := time.Parse(f, s); err == nil {
			t.Time = parsed.UTC()
			return nil
		}
	}
	return fmt.Errorf("invalid Ghost timestamp %q", s)
}

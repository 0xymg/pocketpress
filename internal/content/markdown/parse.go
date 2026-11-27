// Package markdown converts Markdown documents (optional YAML frontmatter)
// into PocketPress post fields: title/slug/meta plus Lexical content_json and
// sanitized-ready HTML.
package markdown

import (
	"fmt"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MaxDocumentBytes caps a single Markdown document (frontmatter + body).
const MaxDocumentBytes = 2 << 20 // 2 MiB

// Meta is the normalized frontmatter PocketPress recognizes on import.
// UnmarshalYAML accepts common static-site / CMS aliases (Hugo, Jekyll, …);
// unknown keys are ignored. Status defaults to draft when empty; only
// "draft" and "published" are accepted by the import service.
type Meta struct {
	Title          string
	Slug           string
	Excerpt        string
	Status         string // normalized: "", "draft", or "published"
	Featured       bool
	Tags           []string
	Categories     []string
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	PublishedAt    *time.Time // optional historical publish time
}

// ResolvedStatus returns the effective import status after applying status,
// draft, and published_at rules. Empty means the caller should default to
// draft (no publish intent).
func (m Meta) ResolvedStatus() string {
	if s := strings.ToLower(strings.TrimSpace(m.Status)); s != "" {
		return s
	}
	if m.PublishedAt != nil {
		return "published"
	}
	return ""
}

// Document is a parsed Markdown file ready to convert into a post.
type Document struct {
	Filename string
	Meta     Meta
	Body     string // Markdown body without frontmatter
}

// Parse splits optional YAML frontmatter from markdown and returns a
// Document. filename is used only for default title/slug hints (stem of the
// basename); it is not required to end in .md.
func Parse(filename, raw string) (Document, error) {
	if len(raw) > MaxDocumentBytes {
		return Document{}, fmt.Errorf("markdown exceeds %d bytes", MaxDocumentBytes)
	}

	meta, body, err := splitFrontmatter(raw)
	if err != nil {
		return Document{}, err
	}

	return Document{
		Filename: strings.TrimSpace(filename),
		Meta:     meta,
		Body:     body,
	}, nil
}

// DefaultTitle returns a human title when frontmatter has none: first ATX
// H1 in the body, else the filename stem, else "Untitled".
func (d Document) DefaultTitle() string {
	if t := strings.TrimSpace(d.Meta.Title); t != "" {
		return t
	}
	if t, _ := extractLeadingH1(d.Body); t != "" {
		return t
	}
	if stem := filenameStem(d.Filename); stem != "" {
		return stem
	}
	return "Untitled"
}

// BodyWithoutLeadingH1 returns the Markdown body with a leading ATX H1
// removed when that H1 was used as the title (frontmatter title empty).
// When frontmatter supplies a title, the body is left intact.
func (d Document) BodyWithoutLeadingH1() string {
	if strings.TrimSpace(d.Meta.Title) != "" {
		return d.Body
	}
	_, rest := extractLeadingH1(d.Body)
	return rest
}

func splitFrontmatter(raw string) (Meta, string, error) {
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	s = strings.TrimPrefix(s, "\uFEFF")

	if !strings.HasPrefix(s, "---\n") {
		return Meta{}, strings.TrimSpace(s), nil
	}

	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	closingAtEOF := false
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - len("\n---")
			closingAtEOF = true
		} else if rest == "---" {
			return Meta{}, "", nil
		} else {
			// Opening fence without a closing fence — treat as plain body.
			return Meta{}, strings.TrimSpace(s), nil
		}
	}

	yamlSrc := rest[:end]
	var body string
	if !closingAtEOF {
		body = rest[end+len("\n---\n"):]
	}

	var meta Meta
	if strings.TrimSpace(yamlSrc) != "" {
		if err := yaml.Unmarshal([]byte(yamlSrc), &meta); err != nil {
			return Meta{}, "", fmt.Errorf("invalid YAML frontmatter: %w", err)
		}
	}
	return meta, strings.TrimSpace(body), nil
}

// UnmarshalYAML maps common frontmatter aliases onto Meta fields.
func (m *Meta) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("frontmatter must be a mapping")
	}
	fields := map[string]*yaml.Node{}
	for i := 0; i+1 < len(value.Content); i += 2 {
		key := strings.ToLower(strings.TrimSpace(value.Content[i].Value))
		if key == "" {
			continue
		}
		// First occurrence wins (later duplicates ignored).
		if _, exists := fields[key]; !exists {
			fields[key] = value.Content[i+1]
		}
	}

	m.Title = firstString(fields, "title")
	m.Slug = slugFromFrontmatter(fields)
	m.Excerpt = firstString(fields, "excerpt", "summary", "custom_excerpt")
	m.SEOTitle = firstString(fields, "seo_title", "meta_title", "og_title")
	m.SEODescription = firstString(fields, "seo_description", "meta_description", "og_description")
	m.CanonicalURL = firstString(fields, "canonical_url", "canonical")

	// description fills excerpt and/or seo_description when those are empty
	// (Hugo/Jekyll often use a single description field for both).
	if desc := firstString(fields, "description"); desc != "" {
		if m.Excerpt == "" {
			m.Excerpt = desc
		}
		if m.SEODescription == "" {
			m.SEODescription = desc
		}
	}

	m.Featured = firstBool(fields, false, "featured", "feature")
	m.Tags = firstStringList(fields, "tags", "tag")
	m.Categories = firstStringList(fields, "categories", "category")

	m.Status = resolveStatusField(fields)
	m.PublishedAt = firstTime(fields, "published_at", "date", "publish_date", "published")

	// If "published" was a boolean, firstTime returns nil; status already set.
	// If "published" was a timestamp and status is still empty, treat as published.
	if m.Status == "" && m.PublishedAt != nil {
		m.Status = "published"
	}

	return nil
}

func resolveStatusField(fields map[string]*yaml.Node) string {
	if raw := firstString(fields, "status"); raw != "" {
		return normalizeStatus(raw)
	}
	if n, ok := fields["draft"]; ok {
		if b, ok := nodeAsBool(n); ok {
			if b {
				return "draft"
			}
			return "published"
		}
	}
	if n, ok := fields["published"]; ok {
		if b, ok := nodeAsBool(n); ok {
			if b {
				return "published"
			}
			return "draft"
		}
	}
	return ""
}

func normalizeStatus(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "draft", "unpublished":
		return "draft"
	case "published", "publish", "public", "live":
		return "published"
	default:
		return strings.ToLower(strings.TrimSpace(raw))
	}
}

func slugFromFrontmatter(fields map[string]*yaml.Node) string {
	if s := firstString(fields, "slug"); s != "" {
		return s
	}
	if permalink := firstString(fields, "permalink", "url", "path"); permalink != "" {
		return slugFromPermalink(permalink)
	}
	return ""
}

func slugFromPermalink(permalink string) string {
	s := strings.TrimSpace(permalink)
	s = strings.TrimSuffix(s, "/")
	if i := strings.Index(s, "?"); i >= 0 {
		s = s[:i]
	}
	if i := strings.Index(s, "#"); i >= 0 {
		s = s[:i]
	}
	base := path.Base(s)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	if base == "." || base == "/" || base == "" {
		return ""
	}
	return base
}

func firstString(fields map[string]*yaml.Node, keys ...string) string {
	for _, k := range keys {
		n, ok := fields[k]
		if !ok || n == nil {
			continue
		}
		if s, ok := nodeAsString(n); ok {
			if s = strings.TrimSpace(s); s != "" {
				return s
			}
		}
	}
	return ""
}

func firstStringList(fields map[string]*yaml.Node, keys ...string) []string {
	for _, k := range keys {
		n, ok := fields[k]
		if !ok || n == nil {
			continue
		}
		var list stringList
		if err := list.UnmarshalYAML(n); err != nil {
			continue
		}
		if len(list) > 0 {
			return []string(list)
		}
	}
	return nil
}

func firstBool(fields map[string]*yaml.Node, fallback bool, keys ...string) bool {
	for _, k := range keys {
		n, ok := fields[k]
		if !ok || n == nil {
			continue
		}
		if b, ok := nodeAsBool(n); ok {
			return b
		}
	}
	return fallback
}

func firstTime(fields map[string]*yaml.Node, keys ...string) *time.Time {
	for _, k := range keys {
		n, ok := fields[k]
		if !ok || n == nil {
			continue
		}
		// Skip boolean scalars (e.g. published: true) — those drive status,
		// not published_at.
		if n.Tag == "!!bool" {
			continue
		}
		if strings.EqualFold(n.Value, "true") || strings.EqualFold(n.Value, "false") {
			continue
		}
		if t, ok := nodeAsTime(n); ok {
			utc := t.UTC()
			return &utc
		}
	}
	return nil
}

func nodeAsString(n *yaml.Node) (string, bool) {
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Tag == "!!null" || n.Value == "~" {
			return "", false
		}
		return n.Value, true
	default:
		return "", false
	}
}

func nodeAsBool(n *yaml.Node) (bool, bool) {
	if n.Kind != yaml.ScalarNode {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(n.Value)) {
	case "true", "yes", "on", "1":
		return true, true
	case "false", "no", "off", "0":
		return false, true
	}
	if n.Tag == "!!bool" {
		b, err := strconv.ParseBool(n.Value)
		return b, err == nil
	}
	return false, false
}

func nodeAsTime(n *yaml.Node) (time.Time, bool) {
	if n.Kind != yaml.ScalarNode {
		return time.Time{}, false
	}
	raw := strings.TrimSpace(n.Value)
	if raw == "" || raw == "~" {
		return time.Time{}, false
	}
	// yaml.v3 may already have decoded timestamps as !!timestamp.
	if n.Tag == "!!timestamp" || n.Tag == "tag:yaml.org,2002:timestamp" {
		if t, err := time.Parse(time.RFC3339, raw); err == nil {
			return t, true
		}
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
		"02 Jan 2006",
		"January 2, 2006",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, raw); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// stringList accepts a YAML sequence or a single string (or comma-separated
// string) so common frontmatter styles all work.
type stringList []string

func (s *stringList) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.SequenceNode:
		var items []string
		if err := value.Decode(&items); err != nil {
			return err
		}
		*s = normalizeStringList(items)
		return nil
	case yaml.ScalarNode:
		raw := strings.TrimSpace(value.Value)
		if raw == "" || value.Tag == "!!null" {
			*s = nil
			return nil
		}
		parts := strings.Split(raw, ",")
		*s = normalizeStringList(parts)
		return nil
	case yaml.DocumentNode, yaml.AliasNode, yaml.MappingNode:
		return fmt.Errorf("tags/categories must be a string or list")
	default:
		if value.Tag == "!!null" || value.Value == "" {
			*s = nil
			return nil
		}
		return fmt.Errorf("tags/categories must be a string or list")
	}
}

func normalizeStringList(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		key := strings.ToLower(s)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, s)
	}
	return out
}

func extractLeadingH1(body string) (title, rest string) {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	i := 0
	for i < len(lines) && strings.TrimSpace(lines[i]) == "" {
		i++
	}
	if i >= len(lines) {
		return "", body
	}
	line := strings.TrimSpace(lines[i])
	// Exactly one leading '#' followed by a space (ATX h1), not ##+.
	if !strings.HasPrefix(line, "# ") || strings.HasPrefix(line, "##") {
		return "", body
	}
	title = strings.TrimSpace(strings.TrimPrefix(line, "# "))
	if title == "" {
		return "", body
	}
	return title, strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
}

func filenameStem(name string) string {
	base := filepath.Base(strings.TrimSpace(name))
	if base == "" || base == "." || base == string(filepath.Separator) {
		return ""
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	stem = strings.ReplaceAll(stem, "_", " ")
	stem = strings.ReplaceAll(stem, "-", " ")
	return strings.Join(strings.Fields(stem), " ")
}

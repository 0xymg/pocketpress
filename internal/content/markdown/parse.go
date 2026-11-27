// Package markdown converts Markdown documents (optional YAML frontmatter)
// into PocketPress post fields: title/slug/meta plus Lexical content_json and
// sanitized-ready HTML.
package markdown

import (
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MaxDocumentBytes caps a single Markdown document (frontmatter + body).
const MaxDocumentBytes = 2 << 20 // 2 MiB

// Meta is the YAML frontmatter PocketPress recognizes on import. Unknown
// keys are ignored. Status defaults to draft when empty; only "draft" and
// "published" are accepted by the import service.
type Meta struct {
	Title          string     `yaml:"title"`
	Slug           string     `yaml:"slug"`
	Excerpt        string     `yaml:"excerpt"`
	Status         string     `yaml:"status"`
	Featured       bool       `yaml:"featured"`
	Tags           stringList `yaml:"tags"`
	Categories     stringList `yaml:"categories"`
	SEOTitle       string     `yaml:"seo_title"`
	SEODescription string     `yaml:"seo_description"`
	CanonicalURL   string     `yaml:"canonical_url"`
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
		if raw == "" {
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
	// Exactly one leading '#" followed by a space (ATX h1), not ##+.
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

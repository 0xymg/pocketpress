// Package wordpress parses WordPress WXR (WordPress eXtended RSS) export XML
// into importable posts.
//
// Only items with wp:post_type "post" are returned. Pages, attachments,
// nav_menu_item and custom types are skipped. Trash items are skipped.
package wordpress

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"time"
)

// MaxExportBytes caps a single WXR export payload.
const MaxExportBytes = 20 << 20 // 20 MiB

// MaxPosts caps how many posts one export may contribute.
const MaxPosts = 200

// Document is one WordPress post ready for PocketPress import.
type Document struct {
	SourceKey      string
	Title          string
	Slug           string
	Excerpt        string
	HTML           string
	Status         string // draft | published
	PublishedAt    *time.Time
	Featured       bool
	Tags           []string
	Categories     []string
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
}

// Parse decodes a WXR export and returns importable post documents.
func Parse(raw []byte) ([]Document, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("WordPress export is empty")
	}
	if len(raw) > MaxExportBytes {
		return nil, fmt.Errorf("WordPress export exceeds %d bytes", MaxExportBytes)
	}

	// Strip a UTF-8 BOM if present — common in WP exports.
	raw = bytes.TrimPrefix(raw, []byte("\ufeff"))

	dec := xml.NewDecoder(bytes.NewReader(raw))
	dec.Strict = false
	dec.CharsetReader = func(charset string, input io.Reader) (io.Reader, error) {
		// Go's xml package handles UTF-8; WP exports are UTF-8. Accept
		// common aliases without converting.
		switch strings.ToLower(charset) {
		case "", "utf-8", "utf8", "us-ascii":
			return input, nil
		default:
			return input, nil
		}
	}

	docs := make([]Document, 0)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("WordPress export is not valid XML")
		}
		se, ok := tok.(xml.StartElement)
		if !ok || !localEq(se.Name, "item") {
			continue
		}
		item, err := readItem(dec, se)
		if err != nil {
			return nil, err
		}
		doc, ok := mapItem(item)
		if !ok {
			continue
		}
		docs = append(docs, doc)
		if len(docs) > MaxPosts {
			return nil, fmt.Errorf("WordPress export has more than %d posts", MaxPosts)
		}
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("WordPress export has no importable posts")
	}
	return docs, nil
}

type wxrItem struct {
	Title       string
	Link        string
	Content     string
	Excerpt     string
	PostName    string
	Status      string
	PostType    string
	PostDateGMT string
	PostDate    string
	IsSticky    string
	Categories  []wxrCat
}

type wxrCat struct {
	Domain   string
	NiceName string
	Name     string
}

func readItem(dec *xml.Decoder, start xml.StartElement) (wxrItem, error) {
	var item wxrItem
	for {
		tok, err := dec.Token()
		if err != nil {
			return item, fmt.Errorf("WordPress export is not valid XML")
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if localEq(t.Name, "item") {
				return item, nil
			}
		case xml.StartElement:
			switch {
			case localEq(t.Name, "title"):
				item.Title, err = readCharData(dec, t)
			case localEq(t.Name, "link"):
				item.Link, err = readCharData(dec, t)
			case localEq(t.Name, "encoded") && spaceLooksLike(t.Name.Space, "excerpt"):
				item.Excerpt, err = readCharData(dec, t)
			case localEq(t.Name, "encoded") && spaceLooksLike(t.Name.Space, "content"):
				item.Content, err = readCharData(dec, t)
			case localEq(t.Name, "post_name"):
				item.PostName, err = readCharData(dec, t)
			case localEq(t.Name, "status"):
				item.Status, err = readCharData(dec, t)
			case localEq(t.Name, "post_type"):
				item.PostType, err = readCharData(dec, t)
			case localEq(t.Name, "post_date_gmt"):
				item.PostDateGMT, err = readCharData(dec, t)
			case localEq(t.Name, "post_date"):
				item.PostDate, err = readCharData(dec, t)
			case localEq(t.Name, "is_sticky"):
				item.IsSticky, err = readCharData(dec, t)
			case localEq(t.Name, "category"):
				cat, cerr := readCategory(dec, t)
				if cerr != nil {
					return item, cerr
				}
				item.Categories = append(item.Categories, cat)
			default:
				if err = dec.Skip(); err != nil {
					return item, fmt.Errorf("WordPress export is not valid XML")
				}
			}
			if err != nil {
				return item, err
			}
		}
	}
}

func readCategory(dec *xml.Decoder, start xml.StartElement) (wxrCat, error) {
	var cat wxrCat
	for _, a := range start.Attr {
		switch strings.ToLower(a.Name.Local) {
		case "domain":
			cat.Domain = a.Value
		case "nicename":
			cat.NiceName = a.Value
		}
	}
	name, err := readCharData(dec, start)
	if err != nil {
		return cat, err
	}
	cat.Name = name
	return cat, nil
}

func readCharData(dec *xml.Decoder, start xml.StartElement) (string, error) {
	var b strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			return "", fmt.Errorf("WordPress export is not valid XML")
		}
		switch t := tok.(type) {
		case xml.EndElement:
			if t.Name.Local == start.Name.Local && t.Name.Space == start.Name.Space {
				return b.String(), nil
			}
		case xml.CharData:
			b.Write(t)
		case xml.StartElement:
			if err := dec.Skip(); err != nil {
				return "", fmt.Errorf("WordPress export is not valid XML")
			}
		}
	}
}

func mapItem(item wxrItem) (Document, bool) {
	postType := strings.ToLower(strings.TrimSpace(item.PostType))
	if postType != "post" {
		return Document{}, false
	}

	status, publish := mapStatus(item.Status)
	if status == "" {
		return Document{}, false
	}

	title := strings.TrimSpace(item.Title)
	slug := strings.TrimSpace(item.PostName)
	source := slug
	if source == "" {
		source = strings.TrimSpace(item.Link)
	}
	if source == "" {
		source = "untitled"
	}
	if title == "" {
		title = "Untitled"
	}

	var cats, tags []string
	for _, c := range item.Categories {
		name := strings.TrimSpace(c.Name)
		if name == "" {
			name = strings.TrimSpace(c.NiceName)
		}
		if name == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(c.Domain)) {
		case "post_tag", "tag":
			tags = append(tags, name)
		case "category", "":
			cats = append(cats, name)
		default:
			// Skip link_category / other taxonomies.
		}
	}

	var publishedAt *time.Time
	if publish {
		if t, ok := parseWPTime(item.PostDateGMT); ok {
			publishedAt = &t
		} else if t, ok := parseWPTime(item.PostDate); ok {
			publishedAt = &t
		}
	}

	featured := item.IsSticky == "1" || strings.EqualFold(item.IsSticky, "true")

	return Document{
		SourceKey:   source,
		Title:       title,
		Slug:        slug,
		Excerpt:     strings.TrimSpace(item.Excerpt),
		HTML:        item.Content,
		Status:      status,
		PublishedAt: publishedAt,
		Featured:    featured,
		Tags:        dedupeNames(tags),
		Categories:  dedupeNames(cats),
	}, true
}

func mapStatus(raw string) (status string, publish bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "publish":
		return "published", true
	case "draft", "pending", "private", "future", "auto-draft":
		return "draft", false
	case "trash", "inherit":
		return "", false
	default:
		if raw == "" {
			return "draft", false
		}
		return "draft", false
	}
}

func parseWPTime(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "0000-00-00 00:00:00" {
		return time.Time{}, false
	}
	formats := []string{
		"2006-01-02 15:04:05",
		time.RFC3339,
		time.RFC1123Z,
		time.RFC1123,
	}
	for _, f := range formats {
		if t, err := time.Parse(f, raw); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}

func localEq(name xml.Name, local string) bool {
	return strings.EqualFold(name.Local, local)
}

// spaceLooksLike matches expanded namespace URIs and undeclared prefixes
// (Strict=false puts the prefix name in Space).
func spaceLooksLike(space, needle string) bool {
	s := strings.ToLower(strings.TrimSpace(space))
	n := strings.ToLower(needle)
	if s == n {
		return true
	}
	return strings.Contains(s, "/"+n) || strings.HasSuffix(s, n) || strings.Contains(s, n+"/")
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

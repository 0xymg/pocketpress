// Package htmlconv converts HTML fragments into PocketPress Lexical
// content_json (and returns the original HTML for the sanitizer to filter).
// Tables, scripts, styles, and other non-allowlisted structures are skipped
// or unwrapped so the result matches what the admin editor can round-trip.
package htmlconv

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Converted is the editor-ready output of an HTML body.
type Converted struct {
	ContentJSON string
	ContentHTML string
}

// Convert turns an HTML fragment into Lexical content_json. ContentHTML is
// the trimmed input (callers should sanitize before storage via content.Sanitize).
func Convert(rawHTML string) (Converted, error) {
	rawHTML = strings.TrimSpace(rawHTML)
	if rawHTML == "" {
		jsonStr, err := marshalEditorState(nil)
		if err != nil {
			return Converted{}, err
		}
		return Converted{ContentJSON: jsonStr, ContentHTML: ""}, nil
	}

	roots, err := html.ParseFragment(strings.NewReader(rawHTML), &html.Node{
		Type:     html.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err != nil {
		return Converted{}, fmt.Errorf("parse html: %w", err)
	}

	var children []node
	for _, n := range roots {
		blocks, err := convertBlock(n)
		if err != nil {
			return Converted{}, err
		}
		children = append(children, blocks...)
	}
	jsonStr, err := marshalEditorState(children)
	if err != nil {
		return Converted{}, err
	}
	return Converted{
		ContentJSON: jsonStr,
		ContentHTML: rawHTML,
	}, nil
}

func convertBlock(n *html.Node) ([]node, error) {
	if n == nil {
		return nil, nil
	}
	switch n.Type {
	case html.TextNode:
		text := collapseWS(n.Data)
		if text == "" {
			return nil, nil
		}
		return []node{paragraphNode([]node{textNode(text, 0)})}, nil
	case html.ElementNode:
		// continue below
	default:
		return nil, nil
	}

	switch n.DataAtom {
	case atom.P:
		inlines, err := convertInlines(n, 0)
		if err != nil {
			return nil, err
		}
		return []node{paragraphNode(inlines)}, nil

	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		level := 1
		if len(n.Data) == 2 && n.Data[0] == 'h' {
			if v, err := strconv.Atoi(n.Data[1:]); err == nil {
				level = v
			}
		}
		inlines, err := convertInlines(n, 0)
		if err != nil {
			return nil, err
		}
		return []node{headingNode(headingTag(level), inlines)}, nil

	case atom.Blockquote:
		kids, err := convertBlockChildren(n)
		if err != nil {
			return nil, err
		}
		if len(kids) == 0 {
			kids = []node{paragraphNode(nil)}
		}
		return []node{quoteNode(kids)}, nil

	case atom.Ul:
		return []node{convertList(n, false)}, nil
	case atom.Ol:
		return []node{convertList(n, true)}, nil

	case atom.Pre:
		code, lang := extractPreCode(n)
		return []node{codeNode(lang, code)}, nil

	case atom.Hr:
		return []node{hrNode()}, nil

	case atom.Img:
		src := attrVal(n, "src")
		if src == "" {
			return nil, nil
		}
		return []node{imageNode(src, attrVal(n, "alt"))}, nil

	case atom.Br:
		return []node{paragraphNode(nil)}, nil

	case atom.Div, atom.Section, atom.Article, atom.Main, atom.Header, atom.Footer, atom.Figure, atom.Figcaption:
		// Unwrap structural containers into their block children.
		return convertBlockChildren(n)

	case atom.Table, atom.Thead, atom.Tbody, atom.Tfoot, atom.Tr, atom.Td, atom.Th, atom.Script, atom.Style, atom.Noscript, atom.Iframe, atom.Object, atom.Embed, atom.Form, atom.Svg:
		// Not in PocketPress allowlist / Lexical editor set.
		return nil, nil

	case atom.Li:
		// Orphan list item — treat as a paragraph of its inlines.
		inlines, err := convertInlines(n, 0)
		if err != nil {
			return nil, err
		}
		return []node{paragraphNode(inlines)}, nil

	default:
		// Unknown element: if it only has inline-ish content, wrap as paragraph;
		// otherwise unwrap children as blocks.
		if looksInlineOnly(n) {
			inlines, err := convertInlines(n, 0)
			if err != nil {
				return nil, err
			}
			if len(inlines) == 0 {
				return nil, nil
			}
			return []node{paragraphNode(inlines)}, nil
		}
		return convertBlockChildren(n)
	}
}

func convertBlockChildren(parent *html.Node) ([]node, error) {
	var out []node
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		blocks, err := convertBlock(c)
		if err != nil {
			return nil, err
		}
		out = append(out, blocks...)
	}
	return out, nil
}

func convertList(list *html.Node, ordered bool) node {
	var items []node
	value := 1
	for c := list.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode || c.DataAtom != atom.Li {
			continue
		}
		items = append(items, convertListItem(c, value))
		value++
	}
	return listNode(ordered, items)
}

func convertListItem(li *html.Node, value int) node {
	var kids []node
	for c := li.FirstChild; c != nil; c = c.NextSibling {
		switch {
		case c.Type == html.ElementNode && (c.DataAtom == atom.Ul || c.DataAtom == atom.Ol):
			kids = append(kids, convertList(c, c.DataAtom == atom.Ol))
		case c.Type == html.ElementNode && c.DataAtom == atom.P:
			inlines, err := convertInlines(c, 0)
			if err == nil {
				kids = append(kids, inlines...)
			}
		case c.Type == html.TextNode:
			text := collapseWS(c.Data)
			if text != "" {
				kids = append(kids, textNode(text, 0))
			}
		default:
			blocks, err := convertBlock(c)
			if err != nil {
				continue
			}
			for _, b := range blocks {
				if b["type"] == "paragraph" {
					if children, ok := b["children"].([]node); ok {
						kids = append(kids, children...)
						continue
					}
				}
				kids = append(kids, b)
			}
		}
	}
	return listItemNode(value, mergeAdjacentText(kids))
}

func convertInlines(parent *html.Node, format int) ([]node, error) {
	var out []node
	for c := parent.FirstChild; c != nil; c = c.NextSibling {
		nodes, err := convertInline(c, format)
		if err != nil {
			return nil, err
		}
		out = append(out, nodes...)
	}
	return mergeAdjacentText(out), nil
}

func convertInline(n *html.Node, format int) ([]node, error) {
	if n == nil {
		return nil, nil
	}
	switch n.Type {
	case html.TextNode:
		s := n.Data
		// Preserve single spaces; collapse runs of whitespace to one space
		// except when the whole node is only whitespace between blocks —
		// callers pass text nodes that belong inside inline context.
		if strings.TrimSpace(s) == "" {
			if strings.ContainsAny(s, " \t") {
				return []node{textNode(" ", format)}, nil
			}
			return nil, nil
		}
		return []node{textNode(collapseWS(s), format)}, nil
	case html.ElementNode:
		// continue
	default:
		return nil, nil
	}

	switch n.DataAtom {
	case atom.Br:
		return []node{textNode("\n", format)}, nil

	case atom.Strong, atom.B:
		return convertInlines(n, format|formatBold)
	case atom.Em, atom.I:
		return convertInlines(n, format|formatItalic)
	case atom.U:
		return convertInlines(n, format|formatUnderline)
	case atom.S, atom.Strike, atom.Del:
		return convertInlines(n, format|formatStrikethrough)
	case atom.Code:
		text := collectText(n)
		return []node{textNode(text, format|formatCode)}, nil

	case atom.A:
		href := attrVal(n, "href")
		children, err := convertInlines(n, format)
		if err != nil {
			return nil, err
		}
		if href == "" {
			return children, nil
		}
		if len(children) == 0 {
			children = []node{textNode(href, format)}
		}
		return []node{linkNode(href, children)}, nil

	case atom.Img:
		src := attrVal(n, "src")
		if src == "" {
			return nil, nil
		}
		return []node{imageNode(src, attrVal(n, "alt"))}, nil

	case atom.Span, atom.Font, atom.Label, atom.Abbr, atom.Cite, atom.Q, atom.Small, atom.Sub, atom.Sup, atom.Mark, atom.Time:
		return convertInlines(n, format)

	case atom.Script, atom.Style, atom.Noscript:
		return nil, nil

	default:
		// Unwrap unknown inline wrappers.
		return convertInlines(n, format)
	}
}

func extractPreCode(pre *html.Node) (code, lang string) {
	// Prefer a nested <code> child.
	for c := pre.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.ElementNode && c.DataAtom == atom.Code {
			lang = classLanguage(attrVal(c, "class"))
			return collectRawText(c), lang
		}
	}
	lang = classLanguage(attrVal(pre, "class"))
	return collectRawText(pre), lang
}

func classLanguage(class string) string {
	for _, part := range strings.Fields(class) {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "language-") {
			return strings.TrimPrefix(part, "language-")
		}
		if strings.HasPrefix(part, "lang-") {
			return strings.TrimPrefix(part, "lang-")
		}
	}
	return ""
}

func collectText(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n == nil {
			return
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

func collectRawText(n *html.Node) string {
	return strings.TrimRight(collectText(n), "\n")
}

func attrVal(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if strings.EqualFold(a.Key, key) {
			return a.Val
		}
	}
	return ""
}

func collapseWS(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func looksInlineOnly(n *html.Node) bool {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type != html.ElementNode {
			continue
		}
		switch c.DataAtom {
		case atom.P, atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6,
			atom.Ul, atom.Ol, atom.Li, atom.Blockquote, atom.Pre, atom.Hr,
			atom.Table, atom.Div, atom.Section, atom.Article:
			return false
		}
	}
	return true
}

// ValidLexicalJSON reports whether s looks like a PocketPress/Ghost Lexical
// editor state with a root node (used to prefer Ghost's lexical field).
func ValidLexicalJSON(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return false
	}
	var payload struct {
		Root map[string]any `json:"root"`
	}
	if err := json.Unmarshal([]byte(s), &payload); err != nil {
		return false
	}
	if payload.Root == nil {
		return false
	}
	t, _ := payload.Root["type"].(string)
	return t == "root"
}

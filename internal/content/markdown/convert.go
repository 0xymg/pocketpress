package markdown

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/renderer/html"
	"github.com/yuin/goldmark/text"
)

// Converted is the editor-ready output of a Markdown body.
type Converted struct {
	ContentJSON string
	ContentHTML string
}

// Convert turns a Markdown body into Lexical content_json and HTML suitable
// for PocketPress's sanitizer allowlist (headings, lists, quotes, code,
// links, images, hr, inline emphasis). GFM strikethrough is supported;
// tables and task lists are skipped (not in the HTML allowlist / Lexical
// editor set).
func Convert(body string) (Converted, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		jsonStr, err := marshalEditorState(nil)
		if err != nil {
			return Converted{}, err
		}
		return Converted{ContentJSON: jsonStr, ContentHTML: ""}, nil
	}

	md := goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithRendererOptions(html.WithHardWraps()),
	)

	source := []byte(body)
	doc := md.Parser().Parse(text.NewReader(source))

	var htmlBuf bytes.Buffer
	if err := md.Renderer().Render(&htmlBuf, source, doc); err != nil {
		return Converted{}, fmt.Errorf("render markdown html: %w", err)
	}

	children, err := convertBlockChildren(doc, source)
	if err != nil {
		return Converted{}, err
	}
	jsonStr, err := marshalEditorState(children)
	if err != nil {
		return Converted{}, err
	}

	return Converted{
		ContentJSON: jsonStr,
		ContentHTML: strings.TrimSpace(htmlBuf.String()),
	}, nil
}

func convertBlockChildren(parent ast.Node, source []byte) ([]node, error) {
	var out []node
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		nodes, err := convertBlock(c, source)
		if err != nil {
			return nil, err
		}
		out = append(out, nodes...)
	}
	return out, nil
}

func convertBlock(n ast.Node, source []byte) ([]node, error) {
	switch t := n.(type) {
	case *ast.Heading:
		inlines, err := convertInlines(t, source, 0)
		if err != nil {
			return nil, err
		}
		return []node{headingNode(headingTag(t.Level), inlines)}, nil

	case *ast.Paragraph:
		inlines, err := convertInlines(t, source, 0)
		if err != nil {
			return nil, err
		}
		return []node{paragraphNode(inlines)}, nil

	case *ast.Blockquote:
		// Lexical QuoteNode holds element children (typically paragraphs).
		kids, err := convertBlockChildren(t, source)
		if err != nil {
			return nil, err
		}
		if len(kids) == 0 {
			kids = []node{paragraphNode(nil)}
		}
		return []node{quoteNode(kids)}, nil

	case *ast.List:
		return []node{convertList(t, source)}, nil

	case *ast.FencedCodeBlock:
		lang := ""
		if t.Info != nil {
			lang = strings.TrimSpace(string(t.Info.Segment.Value(source)))
			if i := strings.IndexAny(lang, " \t"); i >= 0 {
				lang = lang[:i]
			}
		}
		code := fencedCodeText(t, source)
		return []node{codeNode(lang, code)}, nil

	case *ast.CodeBlock:
		code := fencedCodeText(t, source)
		return []node{codeNode("", code)}, nil

	case *ast.ThematicBreak:
		return []node{hrNode()}, nil

	case *ast.HTMLBlock:
		// Raw HTML blocks are not imported into Lexical; skip them. The HTML
		// renderer still emits them and the sanitizer will strip unsafe bits.
		return nil, nil

	case *east.Table:
		// Tables are not in PocketPress's Lexical/HTML allowlist — skip.
		return nil, nil

	default:
		// Unknown block: try paragraphs of text content.
		if n.HasBlankPreviousLines() || n.Type() == ast.TypeBlock {
			textContent := strings.TrimSpace(string(n.Text(source)))
			if textContent == "" {
				return nil, nil
			}
			return []node{paragraphNode([]node{textNode(textContent, 0)})}, nil
		}
		return nil, nil
	}
}

func convertList(list *ast.List, source []byte) node {
	ordered := list.IsOrdered()
	var items []node
	value := 1
	if ordered && list.Start > 0 {
		value = list.Start
	}
	for c := list.FirstChild(); c != nil; c = c.NextSibling() {
		li, ok := c.(*ast.ListItem)
		if !ok {
			continue
		}
		items = append(items, convertListItem(li, source, value, ordered))
		value++
	}
	return listNode(ordered, items)
}

func convertListItem(li *ast.ListItem, source []byte, value int, _ bool) node {
	var kids []node
	for c := li.FirstChild(); c != nil; c = c.NextSibling() {
		switch t := c.(type) {
		case *ast.Paragraph:
			inlines, err := convertInlines(t, source, 0)
			if err == nil {
				kids = append(kids, inlines...)
			}
		case *ast.List:
			kids = append(kids, convertList(t, source))
		default:
			blocks, err := convertBlock(c, source)
			if err == nil {
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
	}
	return listItemNode(value, mergeAdjacentText(kids))
}

func convertInlines(parent ast.Node, source []byte, format int) ([]node, error) {
	var out []node
	for c := parent.FirstChild(); c != nil; c = c.NextSibling() {
		nodes, err := convertInline(c, source, format)
		if err != nil {
			return nil, err
		}
		out = append(out, nodes...)
	}
	return mergeAdjacentText(out), nil
}

func convertInline(n ast.Node, source []byte, format int) ([]node, error) {
	switch t := n.(type) {
	case *ast.Text:
		s := string(t.Segment.Value(source))
		if t.SoftLineBreak() {
			s += "\n"
		}
		if t.HardLineBreak() {
			s += "\n"
		}
		if s == "" {
			return nil, nil
		}
		return []node{textNode(s, format)}, nil

	case *ast.String:
		s := string(t.Value)
		if s == "" {
			return nil, nil
		}
		return []node{textNode(s, format)}, nil

	case *ast.CodeSpan:
		s := string(t.Text(source))
		return []node{textNode(s, format|formatCode)}, nil

	case *ast.Emphasis:
		next := format
		if t.Level >= 2 {
			next |= formatBold
		} else {
			next |= formatItalic
		}
		return convertInlines(t, source, next)

	case *east.Strikethrough:
		return convertInlines(t, source, format|formatStrikethrough)

	case *ast.Link:
		dest := string(t.Destination)
		children, err := convertInlines(t, source, format)
		if err != nil {
			return nil, err
		}
		if len(children) == 0 {
			children = []node{textNode(dest, format)}
		}
		return []node{linkNode(dest, children)}, nil

	case *ast.AutoLink:
		url := string(t.URL(source))
		label := string(t.Label(source))
		if label == "" {
			label = url
		}
		return []node{linkNode(url, []node{textNode(label, format)})}, nil

	case *ast.Image:
		src := string(t.Destination)
		alt := string(t.Text(source))
		return []node{imageNode(src, alt)}, nil

	case *ast.RawHTML:
		// Drop raw inline HTML from Lexical; sanitizer handles HTML path.
		return nil, nil

	default:
		if n.Type() == ast.TypeInline {
			return convertInlines(n, source, format)
		}
		return nil, nil
	}
}

func fencedCodeText(n ast.Node, source []byte) string {
	var b strings.Builder
	for i := 0; i < n.Lines().Len(); i++ {
		line := n.Lines().At(i)
		b.Write(line.Value(source))
	}
	return trimTrailingNewlines(b.String())
}

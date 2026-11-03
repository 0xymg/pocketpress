package content

import (
	"strings"

	"golang.org/x/net/html"
)

// blockElements are the (already sanitized) HTML tags that separate one
// paragraph of derived plain text from the next. Everything else — inline
// emphasis, links, inline code — contributes its text to the current
// paragraph without introducing a break.
var blockElements = map[string]bool{
	"p": true, "hr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"ul": true, "ol": true, "li": true,
	"blockquote": true, "pre": true,
}

// HTMLToText derives the plain-text post body (posts.content_text) from
// sanitized HTML. It strips all tags, collapses each paragraph's whitespace
// to single spaces, and joins paragraphs with a blank line ("\n\n").
//
// Block-level boundaries (paragraphs, headings, list items, block quotes,
// code blocks, horizontal rules) start a new paragraph; a <br> becomes a
// single space within the current paragraph. content_text feeds excerpts and
// future search, so exact intra-paragraph whitespace is intentionally not
// preserved. Empty or whitespace-only input returns "".
func HTMLToText(htmlStr string) string {
	if strings.TrimSpace(htmlStr) == "" {
		return ""
	}

	z := html.NewTokenizer(strings.NewReader(htmlStr))
	var (
		blocks []string
		cur    strings.Builder
	)

	// flush finalizes the current paragraph: its whitespace is collapsed to
	// single spaces (strings.Fields drops empties), and it is appended only
	// when non-empty.
	flush := func() {
		if s := strings.Join(strings.Fields(cur.String()), " "); s != "" {
			blocks = append(blocks, s)
		}
		cur.Reset()
	}

	for {
		switch z.Next() {
		case html.ErrorToken:
			// ErrorToken marks end of input (io.EOF) or a parse error; either
			// way there is nothing more to read.
			flush()
			return strings.Join(blocks, "\n\n")
		case html.TextToken:
			cur.Write(z.Text())
		case html.StartTagToken, html.SelfClosingTagToken:
			name, _ := z.TagName()
			switch {
			case string(name) == "br":
				cur.WriteByte(' ')
			case blockElements[string(name)]:
				flush()
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); blockElements[string(name)] {
				flush()
			}
		}
	}
}

package htmlconv

import (
	"encoding/json"
	"strconv"
)

// Lexical text format bitflags (matching Lexical's TextFormatType).
const (
	formatBold          = 1
	formatItalic        = 2
	formatStrikethrough = 4
	formatUnderline     = 8
	formatCode          = 16
)

// node is a serialized Lexical editor node.
type node map[string]any

func rootNode(children []node) node {
	return node{
		"children":  children,
		"direction": "ltr",
		"format":    "",
		"indent":    0,
		"type":      "root",
		"version":   1,
	}
}

func paragraphNode(children []node) node {
	return node{
		"children":  children,
		"direction": "ltr",
		"format":    "",
		"indent":    0,
		"type":      "paragraph",
		"version":   1,
	}
}

func headingNode(tag string, children []node) node {
	return node{
		"children":  children,
		"direction": "ltr",
		"format":    "",
		"indent":    0,
		"type":      "heading",
		"tag":       tag,
		"version":   1,
	}
}

func quoteNode(children []node) node {
	return node{
		"children":  children,
		"direction": "ltr",
		"format":    "",
		"indent":    0,
		"type":      "quote",
		"version":   1,
	}
}

func listNode(ordered bool, children []node) node {
	listType := "bullet"
	tag := "ul"
	if ordered {
		listType = "number"
		tag = "ol"
	}
	return node{
		"children":  children,
		"direction": "ltr",
		"format":    "",
		"indent":    0,
		"type":      "list",
		"listType":  listType,
		"start":     1,
		"tag":       tag,
		"version":   1,
	}
}

func listItemNode(value int, children []node) node {
	return node{
		"children":  children,
		"direction": "ltr",
		"format":    "",
		"indent":    0,
		"type":      "listitem",
		"value":     value,
		"version":   1,
	}
}

func codeNode(language, text string) node {
	children := []node{}
	if text != "" {
		children = append(children, textNode(text, 0))
	}
	n := node{
		"children":  children,
		"direction": nil,
		"format":    "",
		"indent":    0,
		"type":      "code",
		"version":   1,
	}
	if language != "" {
		n["language"] = language
	}
	return n
}

func linkNode(url string, children []node) node {
	return node{
		"children":  children,
		"direction": "ltr",
		"format":    "",
		"indent":    0,
		"type":      "link",
		"rel":       nil,
		"target":    nil,
		"title":     nil,
		"url":       url,
		"version":   1,
	}
}

func textNode(text string, format int) node {
	return node{
		"detail":  0,
		"format":  format,
		"mode":    "normal",
		"style":   "",
		"text":    text,
		"type":    "text",
		"version": 1,
	}
}

func imageNode(src, alt string) node {
	return node{
		"type":    "image",
		"version": 1,
		"src":     src,
		"altText": alt,
	}
}

func hrNode() node {
	return node{
		"type":    "horizontalrule",
		"version": 1,
	}
}

func marshalEditorState(children []node) (string, error) {
	if len(children) == 0 {
		children = []node{paragraphNode(nil)}
	}
	payload := map[string]any{"root": rootNode(children)}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func headingTag(level int) string {
	if level < 1 {
		level = 1
	}
	if level > 6 {
		level = 6
	}
	return "h" + strconv.Itoa(level)
}

func mergeAdjacentText(nodes []node) []node {
	if len(nodes) < 2 {
		return nodes
	}
	out := make([]node, 0, len(nodes))
	for _, n := range nodes {
		if n["type"] != "text" || len(out) == 0 {
			out = append(out, n)
			continue
		}
		prev := out[len(out)-1]
		if prev["type"] != "text" {
			out = append(out, n)
			continue
		}
		if prev["format"] != n["format"] || prev["mode"] != n["mode"] || prev["style"] != n["style"] {
			out = append(out, n)
			continue
		}
		prev["text"] = prev["text"].(string) + n["text"].(string)
	}
	return out
}

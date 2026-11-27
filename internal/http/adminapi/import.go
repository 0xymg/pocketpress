package adminapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/content/markdown"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
)

// maxImportBodyBytes caps JSON import bodies (many documents × 2 MiB would
// be huge; keep the request under 10 MiB and rely on MaxImportDocuments).
const maxImportBodyBytes = 10 << 20 // 10 MiB

// maxImportMultipartBytes caps multipart Markdown uploads.
const maxImportMultipartBytes = 10 << 20 // 10 MiB

// importMarkdown (POST /posts/import/markdown) creates posts from Markdown
// documents with optional YAML frontmatter. Accepts application/json
// {"documents":[{"filename","markdown"}]} or multipart/form-data with one
// or more "files" parts (.md / .markdown).
//
// Response 200: {"imported":[...], "failed":[...]} — partial success is
// intentional so a bulk import can continue past one bad file.
func (h *Handler) importMarkdown(w http.ResponseWriter, r *http.Request) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}

	items, ok := h.readMarkdownImportItems(w, r)
	if !ok {
		return
	}

	results, err := h.content.ImportMarkdown(r.Context(), items, user.ID)
	if err != nil {
		var ve *content.ValidationError
		if errors.As(err, &ve) {
			h.validationError(w, ve.Fields)
			return
		}
		h.serverError(w, err)
		return
	}

	imported := make([]map[string]any, 0)
	failed := make([]map[string]any, 0)
	for _, res := range results {
		filename := res.Filename
		if res.Post != nil && res.Message == "" && res.Error == nil {
			imported = append(imported, map[string]any{
				"filename": filename,
				"post":     toPostFullDTO(*res.Post),
			})
			continue
		}
		if res.Post != nil && res.Message != "" {
			// Created but publish (or similar) partially failed — still count
			// as imported with a warning.
			imported = append(imported, map[string]any{
				"filename": filename,
				"post":     toPostFullDTO(*res.Post),
				"warning":  res.Message,
			})
			continue
		}
		entry := map[string]any{"filename": filename}
		if res.Error != nil {
			entry["code"] = "validation_failed"
			entry["message"] = "Validation failed."
			entry["details"] = res.Error.Fields
		} else if res.Message != "" {
			entry["code"] = "import_failed"
			entry["message"] = res.Message
		} else {
			entry["code"] = "import_failed"
			entry["message"] = "Import failed."
		}
		failed = append(failed, entry)
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]any{
		"imported": imported,
		"failed":   failed,
	})
}

func (h *Handler) readMarkdownImportItems(w http.ResponseWriter, r *http.Request) ([]content.MarkdownImportItem, bool) {
	ct := r.Header.Get("Content-Type")
	switch {
	case strings.HasPrefix(ct, "multipart/form-data"):
		return h.readMarkdownMultipart(w, r)
	default:
		return h.readMarkdownJSON(w, r)
	}
}

func (h *Handler) readMarkdownJSON(w http.ResponseWriter, r *http.Request) ([]content.MarkdownImportItem, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportBodyBytes)
	var req struct {
		Documents []struct {
			Filename string `json:"filename"`
			Markdown string `json:"markdown"`
		} `json:"documents"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "Request body is not valid JSON.")
		return nil, false
	}
	if len(req.Documents) == 0 {
		h.validationError(w, map[string]string{"documents": "At least one Markdown document is required."})
		return nil, false
	}
	items := make([]content.MarkdownImportItem, 0, len(req.Documents))
	for i, d := range req.Documents {
		if strings.TrimSpace(d.Markdown) == "" && strings.TrimSpace(d.Filename) == "" {
			h.validationError(w, map[string]string{
				"documents": "Document " + strconv.Itoa(i) + " is empty.",
			})
			return nil, false
		}
		items = append(items, content.MarkdownImportItem{
			Filename: d.Filename,
			Markdown: d.Markdown,
		})
	}
	return items, true
}

func (h *Handler) readMarkdownMultipart(w http.ResponseWriter, r *http.Request) ([]content.MarkdownImportItem, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportMultipartBytes+multipartOverhead)
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "Could not parse multipart form.")
		return nil, false
	}
	files := r.MultipartForm.File["files"]
	if len(files) == 0 {
		// Also accept a single "file" field for convenience.
		files = r.MultipartForm.File["file"]
	}
	if len(files) == 0 {
		h.validationError(w, map[string]string{"files": "At least one Markdown file is required."})
		return nil, false
	}
	if len(files) > content.MaxImportDocuments {
		h.validationError(w, map[string]string{
			"files": "At most 50 Markdown documents can be imported at once.",
		})
		return nil, false
	}

	items := make([]content.MarkdownImportItem, 0, len(files))
	for _, fh := range files {
		name := filepath.Base(fh.Filename)
		if name != "" && !content.IsMarkdownFilename(name) {
			h.validationError(w, map[string]string{
				"files": "Only .md and .markdown files are accepted (" + name + ").",
			})
			return nil, false
		}
		if fh.Size > markdown.MaxDocumentBytes {
			h.validationError(w, map[string]string{
				"files": name + " exceeds the 2 MiB per-document limit.",
			})
			return nil, false
		}
		f, err := fh.Open()
		if err != nil {
			h.serverError(w, err)
			return nil, false
		}
		raw, err := io.ReadAll(io.LimitReader(f, markdown.MaxDocumentBytes+1))
		_ = f.Close()
		if err != nil {
			h.serverError(w, err)
			return nil, false
		}
		if len(raw) > markdown.MaxDocumentBytes {
			h.validationError(w, map[string]string{
				"files": name + " exceeds the 2 MiB per-document limit.",
			})
			return nil, false
		}
		items = append(items, content.MarkdownImportItem{
			Filename: name,
			Markdown: string(raw),
		})
	}
	return items, true
}

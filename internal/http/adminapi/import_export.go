package adminapi

import (
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
)

// maxExportImportBytes caps a single Ghost JSON or WordPress WXR upload
// (aligned with ghost.MaxExportBytes / wordpress.MaxExportBytes).
const maxExportImportBytes = 20 << 20 // 20 MiB

// importGhost (POST /posts/import/ghost) creates posts from a Ghost content
// export JSON file. Accepts multipart/form-data with a single "file" (or
// "files") part, or raw application/json body.
//
// Response 200: {"imported":[...], "failed":[...]} — partial success is
// intentional so a bulk import can continue past one bad post.
func (h *Handler) importGhost(w http.ResponseWriter, r *http.Request) {
	h.importExport(w, r, "ghost", func(raw []byte, userID string) ([]content.ExportImportResult, error) {
		return h.content.ImportGhost(r.Context(), raw, userID)
	}, isGhostFilename)
}

// importWordPress (POST /posts/import/wordpress) creates posts from a
// WordPress WXR export. Accepts multipart/form-data with a single "file"
// (or "files") part, or raw application/xml / text/xml body.
func (h *Handler) importWordPress(w http.ResponseWriter, r *http.Request) {
	h.importExport(w, r, "wordpress", func(raw []byte, userID string) ([]content.ExportImportResult, error) {
		return h.content.ImportWordPress(r.Context(), raw, userID)
	}, isWordPressFilename)
}

type exportImporter func(raw []byte, userID string) ([]content.ExportImportResult, error)

func (h *Handler) importExport(w http.ResponseWriter, r *http.Request, kind string, run exportImporter, acceptFile func(string) bool) {
	user, ok := middleware.UserFromContext(r.Context())
	if !ok {
		middleware.WriteError(w, http.StatusUnauthorized, "unauthorized", "Authentication required.")
		return
	}

	raw, filename, ok := h.readExportFile(w, r, kind, acceptFile)
	if !ok {
		return
	}
	_ = filename

	results, err := run(raw, user.ID)
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
		source := res.SourceKey
		if res.Post != nil && res.Message == "" && res.Error == nil {
			imported = append(imported, map[string]any{
				"source_key": source,
				"post":       toPostFullDTO(*res.Post),
			})
			continue
		}
		if res.Post != nil && res.Message != "" {
			imported = append(imported, map[string]any{
				"source_key": source,
				"post":       toPostFullDTO(*res.Post),
				"warning":    res.Message,
			})
			continue
		}
		entry := map[string]any{"source_key": source}
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

func (h *Handler) readExportFile(w http.ResponseWriter, r *http.Request, kind string, acceptFile func(string) bool) ([]byte, string, bool) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		return h.readExportMultipart(w, r, kind, acceptFile)
	}

	label := "Ghost export JSON"
	if kind == "wordpress" {
		label = "WordPress WXR export"
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxExportImportBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			h.validationError(w, map[string]string{"file": label + " exceeds the 20 MiB limit."})
			return nil, "", false
		}
		h.serverError(w, err)
		return nil, "", false
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		h.validationError(w, map[string]string{"file": label + " is required."})
		return nil, "", false
	}
	return raw, "", true
}

func (h *Handler) readExportMultipart(w http.ResponseWriter, r *http.Request, kind string, acceptFile func(string) bool) ([]byte, string, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxExportImportBytes+multipartOverhead)
	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "Could not parse multipart form.")
		return nil, "", false
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		files = r.MultipartForm.File["files"]
	}
	if len(files) == 0 {
		h.validationError(w, map[string]string{"file": "An export file is required."})
		return nil, "", false
	}
	if len(files) > 1 {
		h.validationError(w, map[string]string{"file": "Upload a single export file."})
		return nil, "", false
	}
	fh := files[0]
	name := filepath.Base(fh.Filename)
	if name != "" && !acceptFile(name) {
		msg := "Only .json Ghost export files are accepted."
		if kind == "wordpress" {
			msg = "Only .xml WordPress export files are accepted."
		}
		h.validationError(w, map[string]string{"file": msg + " (" + name + ")."})
		return nil, "", false
	}
	if fh.Size > maxExportImportBytes {
		h.validationError(w, map[string]string{"file": name + " exceeds the 20 MiB limit."})
		return nil, "", false
	}
	f, err := fh.Open()
	if err != nil {
		h.serverError(w, err)
		return nil, "", false
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxExportImportBytes+1))
	_ = f.Close()
	if err != nil {
		h.serverError(w, err)
		return nil, "", false
	}
	if len(raw) > maxExportImportBytes {
		h.validationError(w, map[string]string{"file": name + " exceeds the 20 MiB limit."})
		return nil, "", false
	}
	return raw, name, true
}

func isGhostFilename(name string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	return ext == ".json" || ext == ""
}

func isWordPressFilename(name string) bool {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(name)))
	return ext == ".xml" || ext == ".wxr" || ext == ""
}

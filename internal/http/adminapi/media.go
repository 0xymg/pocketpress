package adminapi

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/media"
	"github.com/pocketpress/pocketpress/internal/store"
)

// Pagination bounds for the media list (frozen contract).
const (
	defaultMediaPerPage = 40
	maxMediaPerPage     = 100
)

// multipartOverhead is the slack http.MaxBytesReader and
// ParseMultipartForm allow above the declared upload size cap, so a file at
// exactly the cap isn't rejected purely for the surrounding multipart
// boundary/field/header bytes.
const multipartOverhead = 64 << 10 // 64 KiB

// multipartMemory bounds how much of the multipart body ParseMultipartForm
// keeps in memory before spilling to a temp file; it is not itself a size
// limit (MaxBytesReader already enforces the real cap on the request body).
const multipartMemory = 10 << 20 // 10 MiB

// mediaDTO is the frozen MEDIA shape.
type mediaDTO struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	Filename  string    `json:"filename"`
	Mime      string    `json:"mime"`
	Size      int64     `json:"size"`
	Width     *int      `json:"width"`
	Height    *int      `json:"height"`
	Alt       string    `json:"alt"`
	CreatedAt time.Time `json:"created_at"`
}

func toMediaDTO(m store.Media) mediaDTO {
	return mediaDTO{
		ID:        m.ID,
		URL:       mediaFileURL(m.ID, m.OriginalName),
		Filename:  m.OriginalName,
		Mime:      m.Mime,
		Size:      m.Size,
		Width:     m.Width,
		Height:    m.Height,
		Alt:       m.Alt,
		CreatedAt: m.CreatedAt,
	}
}

// mediaFileURL builds the public URL for a stored media object: "id" is
// what GET /api/files/{id}/{name...} actually uses to resolve the file; the
// url-escaped original name riding along after it is purely cosmetic (a
// human- and browser-download-friendly filename).
func mediaFileURL(id, originalName string) string {
	return "/api/files/" + id + "/" + url.PathEscape(originalName)
}

// writeMedia writes {"media": MEDIA} with the given status.
func (h *Handler) writeMedia(w http.ResponseWriter, status int, m store.Media) {
	middleware.WriteJSON(w, status, map[string]mediaDTO{"media": toMediaDTO(m)})
}

// handleMediaError maps errors from the media service to the frozen HTTP
// responses: a *media.ValidationError becomes the 400 validation_failed
// envelope, media.ErrMediaInUse becomes 400 "media_in_use", store.ErrNotFound
// becomes a 404 with notFoundMessage, and anything else becomes a generic
// 500.
func (h *Handler) handleMediaError(w http.ResponseWriter, err error, notFoundMessage string) {
	var ve *media.ValidationError
	if errors.As(err, &ve) {
		h.validationError(w, ve.Fields)
		return
	}
	if errors.Is(err, media.ErrMediaInUse) {
		middleware.WriteError(w, http.StatusBadRequest, "media_in_use",
			"This media is used as a post's cover image or Open Graph image.")
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		middleware.WriteError(w, http.StatusNotFound, "not_found", notFoundMessage)
		return
	}
	h.serverError(w, err)
}

// listMedia (GET /media) returns a filtered, paginated page of media,
// newest first.
func (h *Handler) listMedia(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	page := parsePositiveInt(q.Get("page"), 1)
	perPage := parsePositiveInt(q.Get("per_page"), defaultMediaPerPage)
	if perPage > maxMediaPerPage {
		perPage = maxMediaPerPage
	}

	result, err := h.media.List(r.Context(), media.ListQuery{
		Page:    page,
		PerPage: perPage,
		Search:  q.Get("search"),
	})
	if err != nil {
		h.serverError(w, err)
		return
	}

	dtos := make([]mediaDTO, 0, len(result.Media))
	for _, m := range result.Media {
		dtos = append(dtos, toMediaDTO(m))
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]any{
		"media": dtos,
		"meta": map[string]int{
			"page":        result.Page,
			"per_page":    result.PerPage,
			"total_items": result.TotalItems,
			"total_pages": result.TotalPages,
		},
	})
}

// uploadMedia (POST /media) accepts a single multipart "file" field,
// validates it and stores it.
func (h *Handler) uploadMedia(w http.ResponseWriter, r *http.Request) {
	maxBytes := h.media.MaxUploadBytes()
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+multipartOverhead)

	if err := r.ParseMultipartForm(multipartMemory); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.validationError(w, map[string]string{
				"file": fmt.Sprintf("The uploaded file exceeds the %d MB limit.", h.media.MaxUploadMB()),
			})
			return
		}
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "Request body is not valid multipart form data.")
		return
	}
	if r.MultipartForm != nil {
		defer func() { _ = r.MultipartForm.RemoveAll() }()
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		h.validationError(w, map[string]string{"file": "A file is required."})
		return
	}
	defer file.Close()

	user, _ := middleware.UserFromContext(r.Context())
	m, err := h.media.Upload(r.Context(), media.UploadInput{
		Filename:   header.Filename,
		Reader:     file,
		UploaderID: user.ID,
	})
	if err != nil {
		h.handleMediaError(w, err, "Media not found.")
		return
	}
	h.writeMedia(w, http.StatusCreated, m)
}

// updateMediaAlt (PATCH /media/{id}) updates a media row's alt text.
func (h *Handler) updateMediaAlt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Alt string `json:"alt"`
	}
	if !h.decode(w, r, &req) {
		return
	}

	m, err := h.media.UpdateAlt(r.Context(), r.PathValue("id"), req.Alt)
	if err != nil {
		h.handleMediaError(w, err, "Media not found.")
		return
	}
	h.writeMedia(w, http.StatusOK, m)
}

// deleteMedia (DELETE /media/{id}) removes a media row and its stored file,
// refusing (400 "media_in_use") when a post still references it.
func (h *Handler) deleteMedia(w http.ResponseWriter, r *http.Request) {
	if err := h.media.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.handleMediaError(w, err, "Media not found.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// serveFile (GET /api/files/{id}/{name...}, public — no auth) streams a
// stored media file, optionally resized/re-encoded via w/h/fit/fm/q query
// params (see media.ParseTransformQuery). name is not used for lookup, only
// id is; see the mediaFileURL doc comment.
func (h *Handler) serveFile(w http.ResponseWriter, r *http.Request) {
	xform, err := media.ParseTransformQuery(r.URL.Query())
	if err != nil {
		var ve *media.ValidationError
		if errors.As(err, &ve) {
			h.validationError(w, ve.Fields)
			return
		}
		h.serverError(w, err)
		return
	}

	_, out, err := h.media.OpenTransformed(r.Context(), r.PathValue("id"), xform)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) || errors.Is(err, media.ErrNotFound) {
			middleware.WriteError(w, http.StatusNotFound, "not_found", "File not found.")
			return
		}
		h.serverError(w, err)
		return
	}
	defer out.Body.Close()

	header := w.Header()
	header.Set("Content-Type", out.Mime)
	header.Set("Cache-Control", "public, max-age=31536000, immutable")
	header.Set("X-Content-Type-Options", "nosniff")
	if out.Size > 0 {
		header.Set("Content-Length", strconv.FormatInt(out.Size, 10))
	}

	// The net/http server itself suppresses the response body (while still
	// honoring the Content-Length header above) when the original request
	// method was HEAD, so no separate branch is needed here — and "GET
	// /api/files/..." already matches HEAD requests to the same path (Go
	// 1.22+ ServeMux).
	_, _ = io.Copy(w, out.Body)
}

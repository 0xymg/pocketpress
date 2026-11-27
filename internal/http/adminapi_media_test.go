package http

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Fixtures (mirrors internal/media's test fixtures; kept local to this
// package rather than exported from internal/media purely for test use)
// ---------------------------------------------------------------------------

func tinyTestImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 50), G: uint8(y * 50), B: 200, A: 255})
		}
	}
	return img
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, tinyTestImage()); err != nil {
		t.Fatalf("encode PNG fixture: %v", err)
	}
	return buf.Bytes()
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, tinyTestImage(), nil); err != nil {
		t.Fatalf("encode JPEG fixture: %v", err)
	}
	return buf.Bytes()
}

func gifBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, tinyTestImage(), nil); err != nil {
		t.Fatalf("encode GIF fixture: %v", err)
	}
	return buf.Bytes()
}

// webpBytesB64 is the same small (442-byte), genuinely valid lossless WebP
// fixture used by internal/media's own tests — see that package's
// service_test.go for why this is a hardcoded fixture rather than generated
// in-test (golang.org/x/image/webp only decodes, it cannot encode).
const webpBytesB64 = `` +
	`UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZ` +
	`lgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAX` +
	`FOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5` +
	`I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8y` +
	`ZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wO` +
	`K3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8J` +
	`kfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvs` +
	`hXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==`

func webpBytes(t *testing.T) []byte {
	t.Helper()
	clean := strings.ReplaceAll(webpBytesB64, "\n", "")
	data, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		t.Fatalf("decode WebP fixture: %v", err)
	}
	return data
}

const svgBytes = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`

// ---------------------------------------------------------------------------
// testEnv helpers
// ---------------------------------------------------------------------------

// uploadMediaWith issues a multipart POST /api/admin/v1/media with a single
// "file" field, using client (so both the authenticated default client and
// a bare unauthenticated one can be exercised).
func (e *testEnv) uploadMediaWith(t *testing.T, client *http.Client, filename string, data []byte) *http.Response {
	t.Helper()

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatalf("CreateFormFile(%q): %v", filename, err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatalf("write multipart file data: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req, err := http.NewRequest(http.MethodPost, e.srv.URL+"/api/admin/v1/media", &body)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /media: %v", err)
	}
	return resp
}

func (e *testEnv) uploadMedia(t *testing.T, filename string, data []byte) *http.Response {
	t.Helper()
	return e.uploadMediaWith(t, e.client, filename, data)
}

// createMedia uploads filename/data and asserts a 201, returning the
// decoded "media" object.
func (e *testEnv) createMedia(t *testing.T, filename string, data []byte) map[string]any {
	t.Helper()
	resp := e.uploadMedia(t, filename, data)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("upload %q status = %d, want 201; body = %s", filename, resp.StatusCode, raw)
	}
	media, ok := m["media"].(map[string]any)
	if !ok {
		t.Fatalf("upload response has no media object: %s", raw)
	}
	return media
}

// ---------------------------------------------------------------------------
// Upload
// ---------------------------------------------------------------------------

// TestMediaUploadHappyPath covers all four accepted formats end to end
// (multipart HTTP upload) and the frozen MEDIA response shape.
func TestMediaUploadHappyPath(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	cases := []struct {
		name         string
		filename     string
		data         []byte
		wantMime     string
		wantW, wantH float64
	}{
		{"png", "photo.png", pngBytes(t), "image/png", 4, 3},
		{"jpeg", "photo.jpg", jpegBytes(t), "image/jpeg", 4, 3},
		{"gif", "photo.gif", gifBytes(t), "image/gif", 4, 3},
		{"webp", "photo.webp", webpBytes(t), "image/webp", 75, 100},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := env.createMedia(t, tc.filename, tc.data)

			if m["id"] == nil || m["id"] == "" {
				t.Fatalf("media has no id: %v", m)
			}
			if m["filename"] != tc.filename {
				t.Fatalf("filename = %v, want %v", m["filename"], tc.filename)
			}
			if m["mime"] != tc.wantMime {
				t.Fatalf("mime = %v, want %v", m["mime"], tc.wantMime)
			}
			if m["size"].(float64) != float64(len(tc.data)) {
				t.Fatalf("size = %v, want %d", m["size"], len(tc.data))
			}
			if m["width"] != tc.wantW || m["height"] != tc.wantH {
				t.Fatalf("dimensions = %vx%v, want %vx%v", m["width"], m["height"], tc.wantW, tc.wantH)
			}
			if m["alt"] != "" {
				t.Fatalf("alt = %v, want empty on upload", m["alt"])
			}
			wantURL := "/api/files/" + m["id"].(string) + "/" + tc.filename
			if m["url"] != wantURL {
				t.Fatalf("url = %v, want %v", m["url"], wantURL)
			}
			if m["created_at"] == nil {
				t.Fatal("created_at is missing")
			}
		})
	}
}

// TestMediaUploadRejectsSVG confirms an SVG upload is rejected (no SVG in
// v0.1, per the development plan).
func TestMediaUploadRejectsSVG(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.uploadMedia(t, "vector.svg", []byte(svgBytes))
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("upload svg = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["file"]; !ok {
		t.Fatalf("upload svg: details missing \"file\": %v", m)
	}
}

// TestMediaUploadRejectsExtensionMismatch confirms content sniffing catches
// a file whose bytes don't match its extension.
func TestMediaUploadRejectsExtensionMismatch(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	resp := env.uploadMedia(t, "photo.jpg", pngBytes(t)) // PNG bytes, .jpg name
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("upload mismatched extension = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["file"]; !ok {
		t.Fatalf("upload mismatched extension: details missing \"file\": %v", m)
	}
}

// TestMediaUploadRejectsUndecodable confirms a truncated/corrupt image
// (accepted extension, sniffable header, but no decodable dimensions) is
// rejected.
func TestMediaUploadRejectsUndecodable(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	truncated := pngBytes(t)[:16]
	resp := env.uploadMedia(t, "broken.png", truncated)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("upload undecodable = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["file"]; !ok {
		t.Fatalf("upload undecodable: details missing \"file\": %v", m)
	}
}

// TestMediaUploadRejectsOversize confirms a file over the configured
// PP_MAX_UPLOAD_MB limit (10 MB in the test environment) is rejected with a
// 400 validation_failed, not a raw connection error.
func TestMediaUploadRejectsOversize(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	oversize := bytes.Repeat([]byte{0}, (10<<20)+1024) // > 10 MB
	resp := env.uploadMedia(t, "huge.png", oversize)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("upload oversize = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["file"]; !ok {
		t.Fatalf("upload oversize: details missing \"file\": %v", m)
	}
}

// TestMediaUploadRequiresAuth confirms the upload endpoint 401s without a
// session.
func TestMediaUploadRequiresAuth(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	anon := &http.Client{}
	resp := env.uploadMediaWith(t, anon, "photo.png", pngBytes(t))
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, m) != "unauthorized" {
		t.Fatalf("upload unauthenticated = %d %s, want 401 unauthorized", resp.StatusCode, errorCode(t, m))
	}
}

// ---------------------------------------------------------------------------
// List / search / pagination
// ---------------------------------------------------------------------------

// TestMediaListSearchAndPagination covers newest-first ordering, search and
// pagination via the HTTP endpoint.
func TestMediaListSearchAndPagination(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	env.createMedia(t, "alpha.png", pngBytes(t))
	env.createMedia(t, "beta.png", pngBytes(t))
	gamma := env.createMedia(t, "gamma-report.png", pngBytes(t))

	resp := env.do(t, http.MethodGet, "/api/admin/v1/media", nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list media status = %d, want 200", resp.StatusCode)
	}
	items := m["media"].([]any)
	meta := m["meta"].(map[string]any)
	if len(items) != 3 || meta["total_items"].(float64) != 3 {
		t.Fatalf("list media = %d items, meta %v, want 3", len(items), meta)
	}
	if items[0].(map[string]any)["id"] != gamma["id"] {
		t.Fatalf("list media first item = %v, want the newest upload (gamma)", items[0])
	}

	resp = env.do(t, http.MethodGet, "/api/admin/v1/media?search=REPORT", nil)
	_, m = readJSON(t, resp)
	items = m["media"].([]any)
	if len(items) != 1 || items[0].(map[string]any)["id"] != gamma["id"] {
		t.Fatalf("list media search=REPORT = %v, want just gamma-report.png", items)
	}

	resp = env.do(t, http.MethodGet, "/api/admin/v1/media?per_page=2", nil)
	_, m = readJSON(t, resp)
	meta = m["meta"].(map[string]any)
	if meta["per_page"].(float64) != 2 || meta["total_pages"].(float64) != 2 {
		t.Fatalf("list media per_page=2 meta = %v, want per_page 2 total_pages 2", meta)
	}

	// per_page above the max (100) is clamped.
	resp = env.do(t, http.MethodGet, "/api/admin/v1/media?per_page=500", nil)
	_, m = readJSON(t, resp)
	if m["meta"].(map[string]any)["per_page"].(float64) != 100 {
		t.Fatalf("list media per_page=500 clamped = %v, want 100", m["meta"].(map[string]any)["per_page"])
	}
}

// ---------------------------------------------------------------------------
// Alt text
// ---------------------------------------------------------------------------

// TestMediaUpdateAlt covers PATCH /media/{id} setting alt text and the 404
// for an unknown id.
func TestMediaUpdateAlt(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	m := env.createMedia(t, "photo.png", pngBytes(t))
	id := m["id"].(string)

	resp := env.do(t, http.MethodPatch, "/api/admin/v1/media/"+id, map[string]any{"alt": "A scenic photo"})
	_, m2 := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch alt status = %d, want 200", resp.StatusCode)
	}
	if got := m2["media"].(map[string]any)["alt"]; got != "A scenic photo" {
		t.Fatalf("alt after patch = %v, want %q", got, "A scenic photo")
	}

	resp = env.do(t, http.MethodPatch, "/api/admin/v1/media/does-not-exist", map[string]any{"alt": "x"})
	_, m2 = readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m2) != "not_found" {
		t.Fatalf("patch alt unknown id = %d %s, want 404 not_found", resp.StatusCode, errorCode(t, m2))
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

// TestMediaDeleteAndInUseGuard covers plain delete, the media_in_use guard
// while a post references the media as its cover image, and delete
// succeeding once the reference is cleared.
func TestMediaDeleteAndInUseGuard(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	unused := env.createMedia(t, "unused.png", pngBytes(t))
	resp := env.do(t, http.MethodDelete, "/api/admin/v1/media/"+unused["id"].(string), nil)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete unused media status = %d, want 204", resp.StatusCode)
	}
	_ = resp.Body.Close()

	// Deleting an unknown id (including one just deleted) is 404.
	resp = env.do(t, http.MethodDelete, "/api/admin/v1/media/"+unused["id"].(string), nil)
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "not_found" {
		t.Fatalf("delete already-deleted media = %d %s, want 404 not_found", resp.StatusCode, errorCode(t, m))
	}

	cover := env.createMedia(t, "cover.png", pngBytes(t))
	post := env.createPost(t, map[string]any{"title": "Has cover", "cover_image": cover["id"]})

	resp = env.do(t, http.MethodDelete, "/api/admin/v1/media/"+cover["id"].(string), nil)
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "media_in_use" {
		t.Fatalf("delete in-use media = %d %s, want 400 media_in_use", resp.StatusCode, errorCode(t, m))
	}

	// Clear the cover, then delete succeeds.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+post["id"].(string), map[string]any{"cover_image": ""})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear cover status = %d, want 200; body = %v", resp.StatusCode, m)
	}
	if postField(t, m, "cover_image") != nil {
		t.Fatalf("cover_image after clearing = %v, want null", postField(t, m, "cover_image"))
	}

	resp = env.do(t, http.MethodDelete, "/api/admin/v1/media/"+cover["id"].(string), nil)
	if resp.StatusCode != http.StatusNoContent {
		raw, _ := readJSON(t, resp)
		t.Fatalf("delete media after clearing cover = %d, want 204; body = %s", resp.StatusCode, raw)
	}
	_ = resp.Body.Close()
}

// ---------------------------------------------------------------------------
// Public file serving
// ---------------------------------------------------------------------------

// TestServeFileHeadersAndBody confirms GET /api/files/{id}/{name} works
// without any auth cookie, streams the exact uploaded bytes, and sets the
// frozen response headers.
func TestServeFileHeadersAndBody(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	data := pngBytes(t)
	m := env.createMedia(t, "photo.png", data)

	anon := &http.Client{}
	resp, err := anon.Get(env.srv.URL + m["url"].(string))
	if err != nil {
		t.Fatalf("GET file url: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("serve file status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want the frozen immutable cache header", got)
	}
	if got := resp.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := resp.Header.Get("Content-Length"); got != strconv.Itoa(len(data)) {
		t.Fatalf("Content-Length = %q, want %d", got, len(data))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read served file body: %v", err)
	}
	if !bytes.Equal(body, data) {
		t.Fatalf("served file body does not match the uploaded bytes (%d vs %d bytes)", len(body), len(data))
	}
}

// TestServeFileNotFound confirms an unknown media id 404s with the standard
// envelope, not a bare filesystem error.
func TestServeFileNotFound(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	anon := &http.Client{}
	resp, err := anon.Get(env.srv.URL + "/api/files/does-not-exist/photo.png")
	if err != nil {
		t.Fatalf("GET missing file: %v", err)
	}
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusNotFound || errorCode(t, m) != "not_found" {
		t.Fatalf("serve missing file = %d %s, want 404 not_found; body = %s", resp.StatusCode, errorCode(t, m), raw)
	}
}

// TestServeFileTransformResize confirms ?w= resizes a PNG and returns
// image/png (auto format for PNG sources).
func TestServeFileTransformResize(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	data := pngBytes(t)
	m := env.createMedia(t, "photo.png", data)

	anon := &http.Client{}
	resp, err := anon.Get(env.srv.URL + m["url"].(string) + "?w=2")
	if err != nil {
		t.Fatalf("GET transformed file: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("transform status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if len(body) == 0 || bytes.Equal(body, data) {
		t.Fatalf("transformed body should differ from the original upload")
	}
}

// TestServeFileTransformValidation confirms invalid transform params return
// the standard validation_failed envelope.
func TestServeFileTransformValidation(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	m := env.createMedia(t, "photo.png", pngBytes(t))

	anon := &http.Client{}
	resp, err := anon.Get(env.srv.URL + m["url"].(string) + "?w=0")
	if err != nil {
		t.Fatalf("GET bad transform: %v", err)
	}
	raw, body := readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, body) != "validation_failed" {
		t.Fatalf("bad transform = %d %s, want 400 validation_failed; body = %s",
			resp.StatusCode, errorCode(t, body), raw)
	}
}

// ---------------------------------------------------------------------------
// Posts integration: cover_image / og_image
// ---------------------------------------------------------------------------

// TestPostCoverAndOGImageSetClearValidate covers setting cover_image/
// og_image on create, the embedded DTO shape, clearing via "" on update,
// preservation when omitted, and the unknown-media-id validation error.
func TestPostCoverAndOGImageSetClearValidate(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	cover := env.createMedia(t, "cover.png", pngBytes(t))
	og := env.createMedia(t, "og.png", pngBytes(t))

	post := env.createPost(t, map[string]any{
		"title":       "With images",
		"cover_image": cover["id"],
		"og_image":    og["id"],
	})

	gotCover, ok := post["cover_image"].(map[string]any)
	if !ok {
		t.Fatalf("post cover_image = %v, want an embedded media object", post["cover_image"])
	}
	if gotCover["id"] != cover["id"] || gotCover["url"] != cover["url"] {
		t.Fatalf("post cover_image = %v, want id/url matching the uploaded media %v", gotCover, cover)
	}
	for _, key := range []string{"id", "url", "alt", "width", "height"} {
		if _, ok := gotCover[key]; !ok {
			t.Fatalf("post cover_image missing key %q: %v", key, gotCover)
		}
	}
	// The embedded shape is intentionally smaller than the full MEDIA DTO.
	for _, key := range []string{"filename", "mime", "size", "created_at"} {
		if _, ok := gotCover[key]; ok {
			t.Fatalf("post cover_image unexpectedly carries full-MEDIA field %q: %v", key, gotCover)
		}
	}

	gotOG, ok := post["og_image"].(map[string]any)
	if !ok || gotOG["id"] != og["id"] {
		t.Fatalf("post og_image = %v, want the uploaded og media", post["og_image"])
	}

	id := post["id"].(string)

	// Omitting cover_image/og_image on a PATCH leaves them unchanged.
	resp := env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{"title": "Renamed"})
	_, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch (title only) status = %d, want 200", resp.StatusCode)
	}
	if postField(t, m, "cover_image").(map[string]any)["id"] != cover["id"] {
		t.Fatalf("cover_image after unrelated patch = %v, want unchanged", postField(t, m, "cover_image"))
	}

	// Sending "" clears cover_image (and leaves og_image untouched).
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{"cover_image": ""})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("clear cover_image status = %d, want 200", resp.StatusCode)
	}
	if postField(t, m, "cover_image") != nil {
		t.Fatalf("cover_image after clearing = %v, want null", postField(t, m, "cover_image"))
	}
	if postField(t, m, "og_image").(map[string]any)["id"] != og["id"] {
		t.Fatalf("og_image after clearing cover_image = %v, want unchanged", postField(t, m, "og_image"))
	}

	// An unknown media id is a field-level validation error on create...
	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{"title": "Bad cover", "cover_image": "nope"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("create with unknown cover_image = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["cover_image"]; !ok {
		t.Fatalf("create with unknown cover_image: details missing \"cover_image\": %v", m)
	}

	resp = env.do(t, http.MethodPost, "/api/admin/v1/posts", map[string]any{"title": "Bad og", "og_image": "nope"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("create with unknown og_image = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
	if _, ok := validationDetails(t, m)["og_image"]; !ok {
		t.Fatalf("create with unknown og_image: details missing \"og_image\": %v", m)
	}

	// ...and on update.
	resp = env.do(t, http.MethodPatch, "/api/admin/v1/posts/"+id, map[string]any{"cover_image": "nope"})
	_, m = readJSON(t, resp)
	if resp.StatusCode != http.StatusBadRequest || errorCode(t, m) != "validation_failed" {
		t.Fatalf("update with unknown cover_image = %d %s, want 400 validation_failed", resp.StatusCode, errorCode(t, m))
	}
}

// TestPostWithoutImagesHasNullCoverAndOG confirms a post created without a
// cover/OG image serializes both as JSON null (not an empty object or an
// omitted key).
func TestPostWithoutImagesHasNullCoverAndOG(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	post := env.createPost(t, map[string]any{"title": "No images"})
	if v, ok := post["cover_image"]; !ok || v != nil {
		t.Fatalf("cover_image = %v (present=%v), want present and null", v, ok)
	}
	if v, ok := post["og_image"]; !ok || v != nil {
		t.Fatalf("og_image = %v (present=%v), want present and null", v, ok)
	}
}

// TestPostDuplicateCopiesImages confirms duplicating a post copies both
// cover_image and og_image.
func TestPostDuplicateCopiesImages(t *testing.T) {
	env := newTestEnv(t, false)
	env.setup(t)

	cover := env.createMedia(t, "cover.png", pngBytes(t))
	og := env.createMedia(t, "og.png", pngBytes(t))
	orig := env.createPost(t, map[string]any{
		"title": "Original", "cover_image": cover["id"], "og_image": og["id"],
	})

	resp := env.do(t, http.MethodPost, "/api/admin/v1/posts/"+orig["id"].(string)+"/duplicate", nil)
	raw, m := readJSON(t, resp)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("duplicate status = %d, want 201; body = %s", resp.StatusCode, raw)
	}
	dup := m["post"].(map[string]any)

	dupCover, ok := dup["cover_image"].(map[string]any)
	if !ok || dupCover["id"] != cover["id"] {
		t.Fatalf("duplicate cover_image = %v, want the copied cover", dup["cover_image"])
	}
	dupOG, ok := dup["og_image"].(map[string]any)
	if !ok || dupOG["id"] != og["id"] {
		t.Fatalf("duplicate og_image = %v, want the copied og image", dup["og_image"])
	}
}

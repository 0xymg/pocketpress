package media_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"log/slog"
	"strings"
	"testing"

	"github.com/pocketpress/pocketpress/internal/media"
	"github.com/pocketpress/pocketpress/internal/store"
)

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// tinyImage builds a small in-memory RGBA image with a couple of distinct
// colors, big enough to be a valid encodable/decodable image for every
// format under test.
func tinyImage() image.Image {
	img := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 50), G: uint8(y * 50), B: 200, A: 255})
		}
	}
	return img
}

func pngFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, tinyImage()); err != nil {
		t.Fatalf("encode PNG fixture: %v", err)
	}
	return buf.Bytes()
}

func jpegFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, tinyImage(), nil); err != nil {
		t.Fatalf("encode JPEG fixture: %v", err)
	}
	return buf.Bytes()
}

func gifFixture(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := gif.Encode(&buf, tinyImage(), nil); err != nil {
		t.Fatalf("encode GIF fixture: %v", err)
	}
	return buf.Bytes()
}

// webpFixtureB64 is a small (442-byte), genuinely valid lossless WebP image
// (75x100 px), base64-encoded so it can be hardcoded as source text.
// golang.org/x/image/webp only decodes WebP — it cannot encode one — so
// unlike the PNG/JPEG/GIF fixtures above, this can't be generated in-test
// and is instead a fixed byte fixture (see the frozen contract's test
// notes). It is a real WebP bitstream, not a synthetic header, so it
// exercises both content-sniffing and image.DecodeConfig for real.
const webpFixtureB64 = `` +
	`UklGRrIBAABXRUJQVlA4TKUBAAAvSsAYAA8w//M///MfeJAkbXvaSG7m8Q3GfYSBJekwQztm/IcZ` +
	`lgwnmWImn2BK7aFmBtnVir6q//8VOkFE/xm4baTIu8c48ArEo6+B3zFKYln3pqClSCKX0begFTAX` +
	`FOLXHSyF8cCNcZEG4OywuA4KVVfJCiArU7GAgJI8+lJP/OKMT/fBAjevg1cYB7YVkFuWga2lyPi5` +
	`I0HFy5YTpWIHg0RZpkniRVW9odHAKOwosWuOGdxIyn2OvaCDvhg/we6TwadPBPbqBV58MsLmMJ8y` +
	`ZnOWk8SRz4N+QoyPL+MnamzMvcE1rHNEr91F9GKZPVUcS9w7PhhH36suB9qPeYb/oLk6cuTiJ0wO` +
	`K3m5h1cKjW6EVZCYMK7dxcKCBdgP9HkKr9gkAO2P8GKZGWVdIAatQa+1IDpt6qyorVwdy01xdW8J` +
	`kfk6xjEXmVQQ+HQdFr6OKhIN34dXWq0+0qr6EJSCeeVLH9+gvGTLyqM65PQ44ihzlTXxQKjKbAvs` +
	`hXgir7Lil9w4L2bvMycmjQcqXaMCO6BlY28i+FOLzbfI1vEqxAhotocAAA==`

func webpFixture(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString(webpFixtureB64)
	if err != nil {
		t.Fatalf("decode WebP fixture: %v", err)
	}
	return data
}

// svgFixture is not a real image the media service should ever accept — SVG
// is explicitly excluded in v0.1 (development plan §10) because it can carry
// executable script content.
const svgFixture = `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`

// ---------------------------------------------------------------------------
// Test setup helpers
// ---------------------------------------------------------------------------

// newTestService builds a media Service backed by a fresh migrated SQLite
// store in t.TempDir() and a local driver rooted alongside it.
func newTestService(t *testing.T) (*media.Service, *store.Store) {
	t.Helper()
	dir := t.TempDir()

	st, err := store.Open(dir)
	if err != nil {
		t.Fatalf("store.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := store.Up(context.Background(), st.DB()); err != nil {
		t.Fatalf("store.Up() error = %v", err)
	}

	driver, err := media.NewLocalDriver(dir)
	if err != nil {
		t.Fatalf("NewLocalDriver() error = %v", err)
	}

	svc := media.NewService(st.DB(), driver, 10, dir, slog.New(slog.DiscardHandler))
	return svc, st
}

func testUser(t *testing.T, st *store.Store) store.User {
	t.Helper()
	u, err := store.CreateUser(context.Background(), st.DB(), store.NewUser{
		Email:        "uploader@example.com",
		PasswordHash: "hash",
		Name:         "Uploader",
		Role:         store.RoleOwner,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}
	return u
}

// ---------------------------------------------------------------------------
// Upload
// ---------------------------------------------------------------------------

// TestUploadHappyPath covers all four accepted formats: extension and
// sniffed MIME agree, dimensions decode, and the resulting media row and
// stored file both hold the right bytes.
func TestUploadHappyPath(t *testing.T) {
	cases := []struct {
		name         string
		filename     string
		data         func(t *testing.T) []byte
		wantMime     string
		wantW, wantH int
	}{
		{"png", "photo.png", pngFixture, "image/png", 4, 3},
		{"jpeg", "photo.jpg", jpegFixture, "image/jpeg", 4, 3},
		{"gif", "photo.gif", gifFixture, "image/gif", 4, 3},
		{"webp", "photo.webp", webpFixture, "image/webp", 75, 100},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, st := newTestService(t)
			user := testUser(t, st)
			data := tc.data(t)

			m, err := svc.Upload(context.Background(), media.UploadInput{
				Filename:   tc.filename,
				Reader:     bytes.NewReader(data),
				UploaderID: user.ID,
			})
			if err != nil {
				t.Fatalf("Upload(%s) error = %v", tc.name, err)
			}

			if m.OriginalName != tc.filename {
				t.Errorf("OriginalName = %q, want %q", m.OriginalName, tc.filename)
			}
			if m.Mime != tc.wantMime {
				t.Errorf("Mime = %q, want %q", m.Mime, tc.wantMime)
			}
			if m.Size != int64(len(data)) {
				t.Errorf("Size = %d, want %d", m.Size, len(data))
			}
			if m.Width == nil || *m.Width != tc.wantW {
				t.Errorf("Width = %v, want %d", m.Width, tc.wantW)
			}
			if m.Height == nil || *m.Height != tc.wantH {
				t.Errorf("Height = %v, want %d", m.Height, tc.wantH)
			}
			if m.Alt != "" {
				t.Errorf("Alt = %q, want empty on upload", m.Alt)
			}
			if m.Storage != media.DriverLocal {
				t.Errorf("Storage = %q, want %q", m.Storage, media.DriverLocal)
			}
			if m.UploadedBy != user.ID {
				t.Errorf("UploadedBy = %q, want %q", m.UploadedBy, user.ID)
			}
			if m.FileKey == "" {
				t.Error("FileKey is empty, want a storage key")
			}

			// The row is retrievable, and the stored file streams back the
			// exact uploaded bytes.
			got, err := store.GetMediaByID(context.Background(), st.DB(), m.ID)
			if err != nil {
				t.Fatalf("GetMediaByID() error = %v", err)
			}
			if got.ID != m.ID {
				t.Fatalf("GetMediaByID() = %+v, want id %q", got, m.ID)
			}

			gotFile, rc, err := svc.Open(context.Background(), m.ID)
			if err != nil {
				t.Fatalf("Open() error = %v", err)
			}
			defer rc.Close()
			if gotFile.Mime != tc.wantMime {
				t.Errorf("Open() mime = %q, want %q", gotFile.Mime, tc.wantMime)
			}
		})
	}
}

// TestUploadRejectsSVG confirms an SVG upload is rejected outright: SVG is
// not in the accepted-extensions allowlist, per the frozen "no SVG in v0.1"
// rule.
func TestUploadRejectsSVG(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Upload(context.Background(), media.UploadInput{
		Filename: "vector.svg",
		Reader:   strings.NewReader(svgFixture),
	})
	assertFileValidationError(t, err)
}

// TestUploadRejectsExtensionMismatch confirms content sniffing catches a
// file whose real content doesn't match its extension — e.g. a PNG's bytes
// saved under a ".jpg" name — even though the extension alone is on the
// allowlist. This is also what stops a disguised SVG (renamed to ".png")
// from slipping through.
func TestUploadRejectsExtensionMismatch(t *testing.T) {
	svc, _ := newTestService(t)

	cases := []struct {
		name     string
		filename string
		data     []byte
	}{
		{"png bytes named .jpg", "photo.jpg", pngFixture(t)},
		{"jpeg bytes named .png", "photo.png", jpegFixture(t)},
		{"svg disguised as .png", "disguised.png", []byte(svgFixture)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Upload(context.Background(), media.UploadInput{
				Filename: tc.filename,
				Reader:   bytes.NewReader(tc.data),
			})
			assertFileValidationError(t, err)
		})
	}
}

// TestUploadRejectsUndecodableImage confirms a file with an accepted
// extension whose sniffed MIME matches, but whose image data is truncated/
// corrupt (so image.DecodeConfig fails), is rejected rather than stored
// with no known dimensions.
func TestUploadRejectsUndecodableImage(t *testing.T) {
	svc, _ := newTestService(t)

	full := pngFixture(t)
	// Keep only the PNG signature/header bytes (enough for DetectContentType
	// to still sniff "image/png") but truncate the rest, so DecodeConfig
	// fails on the incomplete stream.
	truncated := full[:16]

	_, err := svc.Upload(context.Background(), media.UploadInput{
		Filename: "broken.png",
		Reader:   bytes.NewReader(truncated),
	})
	assertFileValidationError(t, err)
}

// TestUploadRejectsEmptyFile confirms a zero-byte upload is rejected.
func TestUploadRejectsEmptyFile(t *testing.T) {
	svc, _ := newTestService(t)

	_, err := svc.Upload(context.Background(), media.UploadInput{
		Filename: "empty.png",
		Reader:   bytes.NewReader(nil),
	})
	assertFileValidationError(t, err)
}

// assertFileValidationError fails the test unless err is a
// *media.ValidationError carrying a "file" field.
func assertFileValidationError(t *testing.T, err error) {
	t.Helper()
	var ve *media.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("error = %v (%T), want a *media.ValidationError", err, err)
	}
	if _, ok := ve.Fields["file"]; !ok {
		t.Fatalf("ValidationError.Fields = %v, want a \"file\" entry", ve.Fields)
	}
}

// ---------------------------------------------------------------------------
// List / search / pagination
// ---------------------------------------------------------------------------

func uploadNamed(t *testing.T, svc *media.Service, filename string) store.Media {
	t.Helper()
	m, err := svc.Upload(context.Background(), media.UploadInput{
		Filename: filename,
		Reader:   bytes.NewReader(pngFixture(t)),
	})
	if err != nil {
		t.Fatalf("Upload(%q) error = %v", filename, err)
	}
	return m
}

// TestListSearchAndPagination covers newest-first ordering, a
// case-insensitive original_name substring search, and page/per_page math.
func TestListSearchAndPagination(t *testing.T) {
	svc, _ := newTestService(t)

	uploadNamed(t, svc, "alpha.png")
	uploadNamed(t, svc, "beta.png")
	gamma := uploadNamed(t, svc, "gamma-report.png")

	// No filter: newest first (gamma, beta, alpha).
	result, err := svc.List(context.Background(), media.ListQuery{Page: 1, PerPage: 40})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if result.TotalItems != 3 || result.TotalPages != 1 {
		t.Fatalf("List() meta = %+v, want total_items 3 total_pages 1", result)
	}
	if len(result.Media) != 3 || result.Media[0].ID != gamma.ID {
		t.Fatalf("List() first item = %+v, want gamma (newest) first", result.Media)
	}

	// Case-insensitive substring search.
	result, err = svc.List(context.Background(), media.ListQuery{Page: 1, PerPage: 40, Search: "REPORT"})
	if err != nil {
		t.Fatalf("List(search) error = %v", err)
	}
	if result.TotalItems != 1 || result.Media[0].ID != gamma.ID {
		t.Fatalf("List(search=REPORT) = %+v, want just gamma-report.png", result.Media)
	}

	// Pagination: per_page 2 over 3 items.
	page1, err := svc.List(context.Background(), media.ListQuery{Page: 1, PerPage: 2})
	if err != nil {
		t.Fatalf("List(page1) error = %v", err)
	}
	if len(page1.Media) != 2 || page1.TotalPages != 2 {
		t.Fatalf("List(page1) = %+v, want 2 items and 2 total pages", page1)
	}
	page2, err := svc.List(context.Background(), media.ListQuery{Page: 2, PerPage: 2})
	if err != nil {
		t.Fatalf("List(page2) error = %v", err)
	}
	if len(page2.Media) != 1 {
		t.Fatalf("List(page2) = %+v, want 1 remaining item", page2)
	}
}

// ---------------------------------------------------------------------------
// Alt text
// ---------------------------------------------------------------------------

// TestUpdateAlt covers setting alt text and the not-found case.
func TestUpdateAlt(t *testing.T) {
	svc, _ := newTestService(t)
	m := uploadNamed(t, svc, "photo.png")

	updated, err := svc.UpdateAlt(context.Background(), m.ID, "A scenic photo")
	if err != nil {
		t.Fatalf("UpdateAlt() error = %v", err)
	}
	if updated.Alt != "A scenic photo" {
		t.Fatalf("UpdateAlt() Alt = %q, want %q", updated.Alt, "A scenic photo")
	}

	if _, err := svc.UpdateAlt(context.Background(), "does-not-exist", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateAlt(unknown id) error = %v, want store.ErrNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// Delete
// ---------------------------------------------------------------------------

// TestDeleteRemovesRowAndFile confirms Delete removes both the DB row and
// the stored file, and 404s a second delete.
func TestDeleteRemovesRowAndFile(t *testing.T) {
	svc, st := newTestService(t)
	m := uploadNamed(t, svc, "photo.png")

	if err := svc.Delete(context.Background(), m.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	if _, err := store.GetMediaByID(context.Background(), st.DB(), m.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetMediaByID() after Delete() error = %v, want store.ErrNotFound", err)
	}
	if _, _, err := svc.Open(context.Background(), m.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Open() after Delete() error = %v, want store.ErrNotFound", err)
	}

	if err := svc.Delete(context.Background(), m.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second Delete() error = %v, want store.ErrNotFound", err)
	}
}

// TestDeleteRefusesWhenInUse confirms Delete refuses (media.ErrMediaInUse)
// to remove media still referenced by a post's cover_image or og_image, and
// succeeds once the reference is cleared.
func TestDeleteRefusesWhenInUse(t *testing.T) {
	svc, st := newTestService(t)
	user := testUser(t, st)
	cover := uploadNamed(t, svc, "cover.png")
	og := uploadNamed(t, svc, "og.png")

	post, err := store.CreatePost(context.Background(), st.DB(), store.NewPost{
		Title:      "Has cover",
		Slug:       "has-cover",
		Status:     store.PostStatusDraft,
		Author:     user.ID,
		CoverImage: cover.ID,
		OGImage:    og.ID,
	})
	if err != nil {
		t.Fatalf("CreatePost() error = %v", err)
	}

	if err := svc.Delete(context.Background(), cover.ID); !errors.Is(err, media.ErrMediaInUse) {
		t.Fatalf("Delete(cover in use) error = %v, want media.ErrMediaInUse", err)
	}
	if err := svc.Delete(context.Background(), og.ID); !errors.Is(err, media.ErrMediaInUse) {
		t.Fatalf("Delete(og in use) error = %v, want media.ErrMediaInUse", err)
	}

	// Clearing the post's references frees both up for deletion.
	if err := store.UpdatePost(context.Background(), st.DB(), post.ID, store.PostUpdate{
		Title:  post.Title,
		Slug:   post.Slug,
		Status: store.PostStatusDraft,
		Author: user.ID,
		// CoverImage/OGImage left "" (zero value): cleared.
	}); err != nil {
		t.Fatalf("UpdatePost() (clear refs) error = %v", err)
	}

	if err := svc.Delete(context.Background(), cover.ID); err != nil {
		t.Fatalf("Delete(cover, no longer in use) error = %v", err)
	}
	if err := svc.Delete(context.Background(), og.ID); err != nil {
		t.Fatalf("Delete(og, no longer in use) error = %v", err)
	}
}

// ---------------------------------------------------------------------------
// MaxUploadBytes / MaxUploadMB
// ---------------------------------------------------------------------------

// TestMaxUploadBytesAndMBDefaults confirms a non-positive maxUploadMB falls
// back to the frozen default of 10, and that MaxUploadBytes/MaxUploadMB
// agree with each other.
func TestMaxUploadBytesAndMBDefaults(t *testing.T) {
	for _, in := range []int{0, -1, -100} {
		svc := media.NewService(nil, nil, in, "", nil)
		if svc.MaxUploadMB() != 10 {
			t.Errorf("NewService(maxUploadMB=%d).MaxUploadMB() = %d, want default 10", in, svc.MaxUploadMB())
		}
		if svc.MaxUploadBytes() != 10*(1<<20) {
			t.Errorf("NewService(maxUploadMB=%d).MaxUploadBytes() = %d, want %d", in, svc.MaxUploadBytes(), 10*(1<<20))
		}
	}

	svc := media.NewService(nil, nil, 25, "", nil)
	if svc.MaxUploadMB() != 25 || svc.MaxUploadBytes() != 25*(1<<20) {
		t.Errorf("NewService(maxUploadMB=25) MaxUploadMB/Bytes = %d/%d, want 25/%d",
			svc.MaxUploadMB(), svc.MaxUploadBytes(), 25*(1<<20))
	}
}

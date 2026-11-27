package media_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/pocketpress/pocketpress/internal/media"
	"github.com/pocketpress/pocketpress/internal/store"
)

func TestParseTransformQuery(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		query   string
		want    media.Transform
		wantErr string // field name expected in ValidationError, or ""
	}{
		{name: "empty", query: "", want: media.Transform{}},
		{
			name:  "width only",
			query: "w=800",
			want:  media.Transform{Width: 800, Fit: media.FitContain, Quality: 82},
		},
		{
			name:  "cover both dims",
			query: "w=100&h=50&fit=cover&fm=jpeg&q=70",
			want:  media.Transform{Width: 100, Height: 50, Fit: media.FitCover, Format: media.FormatJPEG, Quality: 70},
		},
		{
			name:  "jpg alias",
			query: "fm=jpg",
			want:  media.Transform{Fit: media.FitContain, Format: media.FormatJPEG, Quality: 82},
		},
		{name: "bad width", query: "w=0", wantErr: "w"},
		{name: "oversize", query: "w=99999", wantErr: "w"},
		{name: "webp out", query: "fm=webp", wantErr: "fm"},
		{name: "lone fit", query: "fit=contain", wantErr: "w"},
		{name: "cover missing h", query: "w=100&fit=cover", wantErr: "fit"},
		{name: "bad fit", query: "w=10&fit=stretch", wantErr: "fit"},
		{name: "bad q", query: "w=10&q=0", wantErr: "q"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, err := url.ParseQuery(tc.query)
			if err != nil {
				t.Fatalf("ParseQuery: %v", err)
			}
			got, err := media.ParseTransformQuery(q)
			if tc.wantErr != "" {
				var ve *media.ValidationError
				if err == nil {
					t.Fatalf("ParseTransformQuery(%q) err = nil, want field %q", tc.query, tc.wantErr)
				}
				if !errors.As(err, &ve) || ve.Fields[tc.wantErr] == "" {
					t.Fatalf("ParseTransformQuery(%q) err = %v, want ValidationError field %q", tc.query, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseTransformQuery(%q) unexpected err: %v", tc.query, err)
			}
			if got != tc.want {
				t.Fatalf("ParseTransformQuery(%q) = %+v, want %+v", tc.query, got, tc.want)
			}
		})
	}
}

// newTestServiceDir is like newTestService but also returns the dataDir so
// transform-cache tests can assert on <dataDir>/transforms.
func newTestServiceDir(t *testing.T) (*media.Service, *store.Store, string) {
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
	return svc, st, dir
}

func TestOpenTransformedResizeAndCache(t *testing.T) {
	svc, st, dataDir := newTestServiceDir(t)
	u := testUser(t, st)

	uploaded, err := svc.Upload(context.Background(), media.UploadInput{
		Filename:   "photo.png",
		Reader:     bytes.NewReader(pngFixture(t)),
		UploaderID: u.ID,
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	_, out, err := svc.OpenTransformed(context.Background(), uploaded.ID, media.Transform{
		Width: 2, Fit: media.FitContain, Format: media.FormatPNG, Quality: 82,
	})
	if err != nil {
		t.Fatalf("OpenTransformed: %v", err)
	}
	defer out.Body.Close()

	if out.Mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", out.Mime)
	}
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if img.Bounds().Dx() != 2 {
		t.Fatalf("width = %d, want 2 (source was 4x3, no-upscale contain)", img.Bounds().Dx())
	}

	cacheDir := filepath.Join(dataDir, "transforms", uploaded.ID)
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatalf("read transform cache dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("cache entries = %d, want 1", len(entries))
	}

	_, out2, err := svc.OpenTransformed(context.Background(), uploaded.ID, media.Transform{
		Width: 2, Fit: media.FitContain, Format: media.FormatPNG, Quality: 82,
	})
	if err != nil {
		t.Fatalf("OpenTransformed cached: %v", err)
	}
	defer out2.Body.Close()
	data2, _ := io.ReadAll(out2.Body)
	if !bytes.Equal(data, data2) {
		t.Fatalf("cached transform bytes differ from first result")
	}

	if err := svc.Delete(context.Background(), uploaded.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("transform cache dir still present after Delete; err=%v", err)
	}
}

func TestOpenTransformedJPEGReencode(t *testing.T) {
	svc, st := newTestService(t)
	u := testUser(t, st)

	uploaded, err := svc.Upload(context.Background(), media.UploadInput{
		Filename:   "photo.jpg",
		Reader:     bytes.NewReader(jpegFixture(t)),
		UploaderID: u.ID,
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	_, out, err := svc.OpenTransformed(context.Background(), uploaded.ID, media.Transform{
		Width: 2, Format: media.FormatJPEG, Quality: 60,
	})
	if err != nil {
		t.Fatalf("OpenTransformed: %v", err)
	}
	defer out.Body.Close()
	if out.Mime != "image/jpeg" {
		t.Fatalf("mime = %q, want image/jpeg", out.Mime)
	}
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("jpeg.DecodeConfig: %v", err)
	}
	if cfg.Width != 2 {
		t.Fatalf("width = %d, want 2", cfg.Width)
	}
}

func TestOpenTransformedInactivePassthrough(t *testing.T) {
	svc, st := newTestService(t)
	u := testUser(t, st)
	raw := pngFixture(t)

	uploaded, err := svc.Upload(context.Background(), media.UploadInput{
		Filename:   "photo.png",
		Reader:     bytes.NewReader(raw),
		UploaderID: u.ID,
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	_, out, err := svc.OpenTransformed(context.Background(), uploaded.ID, media.Transform{})
	if err != nil {
		t.Fatalf("OpenTransformed: %v", err)
	}
	defer out.Body.Close()
	got, _ := io.ReadAll(out.Body)
	if !bytes.Equal(got, raw) {
		t.Fatalf("inactive transform should stream original bytes")
	}
	if out.Mime != "image/png" {
		t.Fatalf("mime = %q, want image/png", out.Mime)
	}
}

func TestResizeCoverAndFill(t *testing.T) {
	svc, st := newTestService(t)
	u := testUser(t, st)

	// Build a larger source so cover/fill have room to crop.
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode: %v", err)
	}

	uploaded, err := svc.Upload(context.Background(), media.UploadInput{
		Filename:   "wide.png",
		Reader:     &buf,
		UploaderID: u.ID,
	})
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	for _, tc := range []struct {
		fit   string
		wantW int
		wantH int
	}{
		{fit: media.FitCover, wantW: 10, wantH: 10},
		{fit: media.FitFill, wantW: 10, wantH: 10},
		{fit: media.FitContain, wantW: 10, wantH: 5},
	} {
		t.Run(tc.fit, func(t *testing.T) {
			_, out, err := svc.OpenTransformed(context.Background(), uploaded.ID, media.Transform{
				Width: 10, Height: 10, Fit: tc.fit, Format: media.FormatPNG,
			})
			if err != nil {
				t.Fatalf("OpenTransformed: %v", err)
			}
			defer out.Body.Close()
			data, _ := io.ReadAll(out.Body)
			decoded, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if decoded.Bounds().Dx() != tc.wantW || decoded.Bounds().Dy() != tc.wantH {
				t.Fatalf("size = %dx%d, want %dx%d",
					decoded.Bounds().Dx(), decoded.Bounds().Dy(), tc.wantW, tc.wantH)
			}
		})
	}
}

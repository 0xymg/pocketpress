package media

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/image/draw"
)

// Transform query / fit / format limits. Absolute max dimension caps CPU and
// memory for a single resize; quality only applies to JPEG output.
const (
	maxTransformDim    = 4096
	defaultJPEGQuality = 82
	minJPEGQuality     = 1
	maxJPEGQuality     = 100
	transformsSubdir   = "transforms"
)

// Fit modes for on-demand image transforms.
const (
	FitContain = "contain" // aspect-preserving, fit inside the box (default)
	FitCover   = "cover"   // aspect-preserving, fill the box (crop overflow)
	FitFill    = "fill"    // stretch to exact width × height
)

// Output formats supported by transforms. WebP is accepted as an *upload*
// source (decoded via golang.org/x/image/webp) but is not an encode target
// in v0.2 — re-encode as jpeg or png instead.
const (
	FormatJPEG = "jpeg"
	FormatPNG  = "png"
)

// Transform describes an on-demand resize / re-encode request applied when
// serving GET /api/files/{id}/{name}.
type Transform struct {
	Width   int    // 0 = unset
	Height  int    // 0 = unset
	Fit     string // contain | cover | fill
	Format  string // jpeg | png | "" (auto from source)
	Quality int    // JPEG quality 1–100; 0 means defaultJPEGQuality
}

// Active reports whether any transform parameter is set (so the original
// bytes should not be streamed verbatim).
func (t Transform) Active() bool {
	return t.Width > 0 || t.Height > 0 || t.Format != ""
}

// ParseTransformQuery reads w / h / fit / fm / q from a file-serving query
// string. Unknown or empty values for optional params are fine; invalid
// values return a *ValidationError. Lone fit/q without w, h, or fm is
// rejected so a typo can't silently no-op.
func ParseTransformQuery(q url.Values) (Transform, error) {
	rawW := strings.TrimSpace(q.Get("w"))
	rawH := strings.TrimSpace(q.Get("h"))
	rawFit := strings.TrimSpace(q.Get("fit"))
	rawFM := strings.TrimSpace(q.Get("fm"))
	rawQ := strings.TrimSpace(q.Get("q"))

	hasAny := rawW != "" || rawH != "" || rawFit != "" || rawFM != "" || rawQ != ""
	if !hasAny {
		return Transform{}, nil
	}

	t := Transform{Fit: FitContain, Quality: defaultJPEGQuality}

	if rawW != "" {
		w, err := strconv.Atoi(rawW)
		if err != nil || w < 1 || w > maxTransformDim {
			return Transform{}, fieldError("w", fmt.Sprintf("Width must be an integer between 1 and %d.", maxTransformDim))
		}
		t.Width = w
	}
	if rawH != "" {
		h, err := strconv.Atoi(rawH)
		if err != nil || h < 1 || h > maxTransformDim {
			return Transform{}, fieldError("h", fmt.Sprintf("Height must be an integer between 1 and %d.", maxTransformDim))
		}
		t.Height = h
	}

	if rawFit != "" {
		switch strings.ToLower(rawFit) {
		case FitContain, FitCover, FitFill:
			t.Fit = strings.ToLower(rawFit)
		default:
			return Transform{}, fieldError("fit", "Fit must be one of: contain, cover, fill.")
		}
	}

	if rawFM != "" {
		switch strings.ToLower(rawFM) {
		case FormatJPEG, "jpg":
			t.Format = FormatJPEG
		case FormatPNG:
			t.Format = FormatPNG
		case "webp":
			return Transform{}, fieldError("fm", "WebP output is not supported; use jpeg or png.")
		default:
			return Transform{}, fieldError("fm", "Format must be one of: jpeg, png.")
		}
	}

	if rawQ != "" {
		qq, err := strconv.Atoi(rawQ)
		if err != nil || qq < minJPEGQuality || qq > maxJPEGQuality {
			return Transform{}, fieldError("q", fmt.Sprintf("Quality must be an integer between %d and %d.", minJPEGQuality, maxJPEGQuality))
		}
		t.Quality = qq
	}

	if !t.Active() {
		return Transform{}, fieldError("w", "At least one of w, h, or fm is required to transform an image.")
	}
	if (t.Fit == FitCover || t.Fit == FitFill) && (t.Width == 0 || t.Height == 0) {
		return Transform{}, fieldError("fit", "fit=cover and fit=fill require both w and h.")
	}

	return t, nil
}

// cacheKey builds a stable filesystem-safe key for a transform of one media
// object. fileKey is included so a replace-under-same-id (not currently
// supported, but cheap insurance) cannot serve a stale variant.
func (t Transform) cacheKey(fileKey string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d|%d|%s|%s|%d",
		fileKey, t.Width, t.Height, t.Fit, t.Format, t.Quality)))
	return hex.EncodeToString(sum[:16])
}

// applyTransform decodes src, resizes per t, and encodes to the chosen
// format. srcMime hints the auto format when t.Format is empty.
func applyTransform(src []byte, srcMime string, t Transform) (out []byte, mime string, err error) {
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return nil, "", fmt.Errorf("decode image for transform: %w", err)
	}

	format := t.Format
	if format == "" {
		format = autoFormat(srcMime)
	}
	t.Format = format
	if t.Quality <= 0 {
		t.Quality = defaultJPEGQuality
	}
	if t.Fit == "" {
		t.Fit = FitContain
	}

	resized := resizeImage(img, t)
	var buf bytes.Buffer
	switch format {
	case FormatPNG:
		if err := png.Encode(&buf, resized); err != nil {
			return nil, "", fmt.Errorf("encode png: %w", err)
		}
		mime = "image/png"
	default:
		if err := jpeg.Encode(&buf, resized, &jpeg.Options{Quality: t.Quality}); err != nil {
			return nil, "", fmt.Errorf("encode jpeg: %w", err)
		}
		mime = "image/jpeg"
	}
	return buf.Bytes(), mime, nil
}

func autoFormat(srcMime string) string {
	switch srcMime {
	case "image/png", "image/gif":
		return FormatPNG
	default:
		// JPEG and WebP sources default to JPEG; callers who need alpha
		// from a WebP can pass fm=png explicitly.
		return FormatJPEG
	}
}

func resizeImage(src image.Image, t Transform) image.Image {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw < 1 || sh < 1 {
		return src
	}

	tw, th := t.Width, t.Height
	// No upscale: never exceed the source dimensions.
	if tw > sw {
		tw = sw
	}
	if th > sh {
		th = sh
	}
	if tw == 0 && th == 0 {
		return src
	}

	// Single target dimension: scale proportionally (float math so a 4×3
	// image with w=2 becomes 2×2-ish / 2×1.5→2×2 rounded, not truncated).
	if tw == 0 || th == 0 {
		var scale float64
		if tw == 0 {
			scale = float64(th) / float64(sh)
		} else {
			scale = float64(tw) / float64(sw)
		}
		rw := max(1, int(float64(sw)*scale+0.5))
		rh := max(1, int(float64(sh)*scale+0.5))
		dst := image.NewRGBA(image.Rect(0, 0, rw, rh))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Over, nil)
		return dst
	}

	fit := t.Fit
	if fit == "" {
		fit = FitContain
	}

	switch fit {
	case FitFill:
		dst := image.NewRGBA(image.Rect(0, 0, tw, th))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Over, nil)
		return dst
	case FitCover:
		scale := float64(tw) / float64(sw)
		if float64(th)/float64(sh) > scale {
			scale = float64(th) / float64(sh)
		}
		rw := max(1, int(float64(sw)*scale+0.5))
		rh := max(1, int(float64(sh)*scale+0.5))
		tmp := image.NewRGBA(image.Rect(0, 0, rw, rh))
		draw.CatmullRom.Scale(tmp, tmp.Bounds(), src, sb, draw.Over, nil)
		x0 := (rw - tw) / 2
		y0 := (rh - th) / 2
		dst := image.NewRGBA(image.Rect(0, 0, tw, th))
		draw.Draw(dst, dst.Bounds(), tmp, image.Pt(x0, y0), draw.Src)
		return dst
	default: // contain
		scale := float64(tw) / float64(sw)
		if float64(th)/float64(sh) < scale {
			scale = float64(th) / float64(sh)
		}
		rw := max(1, int(float64(sw)*scale+0.5))
		rh := max(1, int(float64(sh)*scale+0.5))
		dst := image.NewRGBA(image.Rect(0, 0, rw, rh))
		draw.CatmullRom.Scale(dst, dst.Bounds(), src, sb, draw.Over, nil)
		return dst
	}
}

// Transformed is the result of Service.OpenTransformed: encoded bytes ready
// to stream, with the response Content-Type.
type Transformed struct {
	Mime string
	Size int64
	Body io.ReadCloser
}

func (s *Service) transformCachePath(mediaID, key, ext string) string {
	return filepath.Join(s.cacheDir, mediaID, key+ext)
}

func (s *Service) readTransformCache(path string) (io.ReadCloser, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, 0, err
	}
	return f, st.Size(), nil
}

func (s *Service) writeTransformCache(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), localDirPerm); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, localFilePerm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Service) clearTransformCache(mediaID string) {
	if s.cacheDir == "" || mediaID == "" {
		return
	}
	dir := filepath.Join(s.cacheDir, mediaID)
	if err := os.RemoveAll(dir); err != nil {
		s.logger.Error("failed to clear transform cache",
			slog.String("media_id", mediaID), slog.String("error", err.Error()))
	}
}

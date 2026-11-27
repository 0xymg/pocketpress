package media

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"image"
	_ "image/gif"  // register GIF dimension decoding
	_ "image/jpeg" // register JPEG dimension decoding
	_ "image/png"  // register PNG dimension decoding
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/webp" // register WebP dimension decoding

	"github.com/google/uuid"

	"github.com/pocketpress/pocketpress/internal/store"
)

// defaultMaxUploadMB is the fallback upload size cap when the service is
// built with a non-positive maxUploadMB (mirrors PP_MAX_UPLOAD_MB's own
// default in internal/app.Config).
const defaultMaxUploadMB = 10

const bytesPerMB = 1 << 20

// allowedExtensions maps a lowercased file extension to the exact MIME type
// PocketPress accepts for it. Content sniffing (http.DetectContentType)
// must agree with this mapping or the upload is rejected — together with
// the extension allowlist itself, this is what keeps an SVG (or anything
// else) from sneaking in disguised under an accepted extension.
var allowedExtensions = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".webp": "image/webp",
	".gif":  "image/gif",
}

// Service is the media domain layer: it validates uploads (extension,
// sniffed content type, decodable dimensions, size), stores accepted files
// via a Driver, and owns the media table (list/search/paginate, alt-text
// updates, delete with the media_in_use guard). HTTP handlers under
// internal/http/adminapi call it; no validation or storage decision is made
// there.
type Service struct {
	db          *sql.DB
	driver      Driver
	storageName string
	logger      *slog.Logger
	maxUploadMB int
	// cacheDir is <PP_DATA_DIR>/transforms: on-demand resize/re-encode
	// variants are cached here regardless of whether originals live on
	// local disk or S3. Empty disables the disk cache (transforms still
	// work; each request re-encodes).
	cacheDir string
}

// NewService builds a media Service. maxUploadMB <= 0 falls back to
// defaultMaxUploadMB. dataDir is PP_DATA_DIR; the transform cache is rooted
// at <dataDir>/transforms (created lazily on first transform). An empty
// dataDir disables disk caching.
func NewService(db *sql.DB, driver Driver, maxUploadMB int, dataDir string, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if maxUploadMB <= 0 {
		maxUploadMB = defaultMaxUploadMB
	}
	cacheDir := ""
	if strings.TrimSpace(dataDir) != "" {
		cacheDir = filepath.Join(dataDir, transformsSubdir)
	}
	return &Service{
		db:          db,
		driver:      driver,
		storageName: storageName(driver),
		logger:      logger,
		maxUploadMB: maxUploadMB,
		cacheDir:    cacheDir,
	}
}

// MaxUploadBytes returns the configured upload size cap in bytes, for the
// HTTP layer's http.MaxBytesReader.
func (s *Service) MaxUploadBytes() int64 {
	return int64(s.maxUploadMB) * bytesPerMB
}

// MaxUploadMB returns the configured upload size cap in whole megabytes,
// for building a human-readable validation message.
func (s *Service) MaxUploadMB() int {
	return s.maxUploadMB
}

// UploadInput is the resolved multipart upload the HTTP layer hands to
// Upload after extracting the "file" form field.
type UploadInput struct {
	// Filename is the client-supplied original filename (used for the
	// extension check and stored verbatim as media.original_name).
	Filename string
	// Reader is the file content. Upload reads it to completion.
	Reader io.Reader
	// UploaderID is the authenticated user's id (media.uploaded_by).
	UploaderID string
}

// Upload validates and stores an uploaded image, inserting its media row.
// Validation failures — an unsupported/mismatched file type or an
// undecodable image — are reported as a *ValidationError with a single
// "file" field, per the frozen contract. The caller (the HTTP handler) is
// responsible for enforcing the overall request size cap (MaxUploadBytes)
// before Reader is handed here.
func (s *Service) Upload(ctx context.Context, in UploadInput) (store.Media, error) {
	ext := strings.ToLower(filepath.Ext(in.Filename))
	expectedMime, ok := allowedExtensions[ext]
	if !ok {
		return store.Media{}, fieldError("file", "Unsupported file type. Allowed types: JPEG, PNG, WebP, GIF.")
	}

	// Buffer the whole upload: the HTTP layer already bounds it via
	// http.MaxBytesReader, so this is a bounded read, and both content
	// sniffing and dimension decoding want to read from the start of the
	// file (dimension decoding, in particular, doesn't know its own bound).
	data, err := io.ReadAll(in.Reader)
	if err != nil {
		return store.Media{}, fmt.Errorf("read upload: %w", err)
	}
	if len(data) == 0 {
		return store.Media{}, fieldError("file", "The uploaded file is empty.")
	}

	sniffed := sniffMime(data)
	if sniffed != expectedMime {
		return store.Media{}, fieldError("file", "File content does not match its extension.")
	}

	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return store.Media{}, fieldError("file", "Unable to read image dimensions.")
	}
	width, height := cfg.Width, cfg.Height

	id, err := newMediaID()
	if err != nil {
		return store.Media{}, err
	}
	key := storageKey(id, ext)

	if err := s.driver.Save(ctx, key, bytes.NewReader(data), int64(len(data)), sniffed); err != nil {
		return store.Media{}, fmt.Errorf("save uploaded file: %w", err)
	}

	m, err := store.CreateMedia(ctx, s.db, store.NewMedia{
		ID:           id,
		FileKey:      key,
		OriginalName: in.Filename,
		Mime:         sniffed,
		Size:         int64(len(data)),
		Width:        &width,
		Height:       &height,
		Alt:          "",
		Storage:      s.storageName,
		UploadedBy:   in.UploaderID,
	})
	if err != nil {
		// The file is already saved but the DB insert failed; best-effort
		// clean it up so a retry doesn't strand an orphan object under a
		// key nothing will ever reference again (the key embeds a fresh id
		// each attempt).
		if delErr := s.driver.Delete(ctx, key); delErr != nil {
			s.logger.Error("failed to roll back orphaned upload after insert failure",
				slog.String("key", key), slog.String("error", delErr.Error()))
		}
		return store.Media{}, fmt.Errorf("insert media row: %w", err)
	}

	s.logger.Info("media uploaded", slog.String("media_id", m.ID), slog.Int64("size", m.Size))
	return m, nil
}

// sniffMime runs http.DetectContentType over data's first 512 bytes (or all
// of it, if shorter), stripping any "; charset=..." suffix DetectContentType
// can append for text-ish content — images never carry one, but this keeps
// the comparison against allowedExtensions exact regardless.
func sniffMime(data []byte) string {
	n := len(data)
	if n > 512 {
		n = 512
	}
	sniffed := http.DetectContentType(data[:n])
	if semi := strings.IndexByte(sniffed, ';'); semi != -1 {
		sniffed = strings.TrimSpace(sniffed[:semi])
	}
	return sniffed
}

// storageKey builds the frozen key layout: "<year>/<month>/<id><ext>",
// grouping uploads by calendar month so no single directory (or S3 "folder")
// accumulates every upload ever made.
func storageKey(id, ext string) string {
	now := time.Now().UTC()
	return fmt.Sprintf("%04d/%02d/%s%s", now.Year(), now.Month(), id, ext)
}

// newMediaID generates a UUIDv7 media id. The media service generates its
// own id (rather than letting store.CreateMedia generate one, as most other
// CreateXxx store functions do) because the id must be known before the row
// exists, to build the storage key the file is saved under.
func newMediaID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("generate media id: %w", err)
	}
	return id.String(), nil
}

// ListQuery is the resolved (already clamped/normalized by the handler)
// query for List.
type ListQuery struct {
	Page    int
	PerPage int
	Search  string // "" = no filter; case-insensitive original_name substring
}

// ListResult is a page of media plus its pagination metadata.
type ListResult struct {
	Media      []store.Media
	Page       int
	PerPage    int
	TotalItems int
	TotalPages int
}

// List returns one page of media, newest first, optionally filtered by q.Search.
func (s *Service) List(ctx context.Context, q ListQuery) (ListResult, error) {
	total, err := store.CountMedia(ctx, s.db, q.Search)
	if err != nil {
		return ListResult{}, err
	}

	offset := (q.Page - 1) * q.PerPage
	items, err := store.ListMedia(ctx, s.db, q.Search, q.PerPage, offset)
	if err != nil {
		return ListResult{}, err
	}

	totalPages := 0
	if q.PerPage > 0 {
		totalPages = (total + q.PerPage - 1) / q.PerPage
	}

	return ListResult{
		Media:      items,
		Page:       q.Page,
		PerPage:    q.PerPage,
		TotalItems: total,
		TotalPages: totalPages,
	}, nil
}

// Get returns a single media row. Returns store.ErrNotFound for an unknown
// id.
func (s *Service) Get(ctx context.Context, id string) (store.Media, error) {
	return store.GetMediaByID(ctx, s.db, id)
}

// UpdateAlt overwrites a media row's alt text. Returns store.ErrNotFound for
// an unknown id.
func (s *Service) UpdateAlt(ctx context.Context, id, alt string) (store.Media, error) {
	if err := store.UpdateMediaAlt(ctx, s.db, id, alt); err != nil {
		return store.Media{}, err
	}
	return store.GetMediaByID(ctx, s.db, id)
}

// Delete removes a media row and its stored file. It refuses (ErrMediaInUse)
// when the media is still referenced by a post's cover_image or og_image.
// Returns store.ErrNotFound for an unknown id.
//
// The DB row is deleted before the stored file: if the driver delete then
// fails, the result is an orphaned file (harmless, cleanable later) rather
// than a DB row pointing at a file that silently vanished.
func (s *Service) Delete(ctx context.Context, id string) error {
	m, err := store.GetMediaByID(ctx, s.db, id)
	if err != nil {
		return err
	}

	inUse, err := store.MediaInUse(ctx, s.db, id)
	if err != nil {
		return err
	}
	if inUse {
		return ErrMediaInUse
	}

	if err := store.DeleteMedia(ctx, s.db, id); err != nil {
		return err
	}

	if err := s.driver.Delete(ctx, m.FileKey); err != nil {
		s.logger.Error("failed to delete stored media file",
			slog.String("media_id", id), slog.String("error", err.Error()))
	}
	s.clearTransformCache(id)

	s.logger.Info("media deleted", slog.String("media_id", id))
	return nil
}

// Open returns a media row and a reader for its stored bytes, for the
// public file-serving route. The caller must Close the reader. Returns
// store.ErrNotFound for an unknown id, or ErrNotFound if the DB row exists
// but the driver has no matching object (e.g. it was deleted out of band).
func (s *Service) Open(ctx context.Context, id string) (store.Media, io.ReadCloser, error) {
	m, err := store.GetMediaByID(ctx, s.db, id)
	if err != nil {
		return store.Media{}, nil, err
	}

	rc, err := s.driver.Open(ctx, m.FileKey)
	if err != nil {
		return store.Media{}, nil, err
	}
	return m, rc, nil
}

// OpenTransformed opens media id and returns either the original bytes (when
// t is inactive) or a resized/re-encoded variant. Cached variants live under
// <dataDir>/transforms/{id}/. The caller must Close Transformed.Body.
func (s *Service) OpenTransformed(ctx context.Context, id string, t Transform) (store.Media, Transformed, error) {
	m, err := store.GetMediaByID(ctx, s.db, id)
	if err != nil {
		return store.Media{}, Transformed{}, err
	}

	if !t.Active() {
		rc, err := s.driver.Open(ctx, m.FileKey)
		if err != nil {
			return store.Media{}, Transformed{}, err
		}
		return m, Transformed{Mime: m.Mime, Size: m.Size, Body: rc}, nil
	}

	// Resolve auto format before the cache key so fm="" and the chosen
	// default share one cache entry.
	resolved := t
	if resolved.Format == "" {
		resolved.Format = autoFormat(m.Mime)
	}
	if resolved.Quality <= 0 {
		resolved.Quality = defaultJPEGQuality
	}
	if resolved.Fit == "" {
		resolved.Fit = FitContain
	}

	mime, ext := "image/jpeg", ".jpg"
	if resolved.Format == FormatPNG {
		mime, ext = "image/png", ".png"
	}

	cachePath := ""
	if s.cacheDir != "" {
		cachePath = s.transformCachePath(m.ID, resolved.cacheKey(m.FileKey), ext)
		if rc, size, err := s.readTransformCache(cachePath); err == nil {
			return m, Transformed{Mime: mime, Size: size, Body: rc}, nil
		} else if !os.IsNotExist(err) {
			s.logger.Error("failed to read transform cache",
				slog.String("media_id", id), slog.String("error", err.Error()))
		}
	}

	rc, err := s.driver.Open(ctx, m.FileKey)
	if err != nil {
		return store.Media{}, Transformed{}, err
	}
	src, err := io.ReadAll(rc)
	_ = rc.Close()
	if err != nil {
		return store.Media{}, Transformed{}, fmt.Errorf("read media for transform: %w", err)
	}

	out, outMime, err := applyTransform(src, m.Mime, resolved)
	if err != nil {
		return store.Media{}, Transformed{}, err
	}

	if cachePath != "" {
		if err := s.writeTransformCache(cachePath, out); err != nil {
			s.logger.Error("failed to write transform cache",
				slog.String("media_id", id), slog.String("error", err.Error()))
		}
	}

	return m, Transformed{
		Mime: outMime,
		Size: int64(len(out)),
		Body: io.NopCloser(bytes.NewReader(out)),
	}, nil
}

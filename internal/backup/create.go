package backup

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// archiveDirPerm and archiveFilePerm are the permissions used for
// directories and files this package creates.
const (
	archiveDirPerm  = 0o755
	archiveFilePerm = 0o644

	// backupTimestampLayout is the layout used both for the default backup
	// filename and for naming a restore's pre-restore-<timestamp> directory.
	backupTimestampLayout = "20060102-150405"
)

// CreateOptions configures Create.
type CreateOptions struct {
	// DataDir is PP_DATA_DIR: the directory containing pocketpress.db and
	// the local uploads/ directory.
	DataDir string

	// Out is the caller-supplied --out value: "" selects the default
	// location (<DataDir>/backups/pocketpress-backup-<timestamp>.zip); a
	// value ending in ".zip" is used verbatim as the archive path; anything
	// else is treated as a directory to auto-name the archive inside of.
	Out string

	// Version is the PocketPress version recorded in the manifest.
	Version string

	// StorageDriver is the configured PP_STORAGE_DRIVER ("local" or "s3"),
	// recorded in the manifest.
	StorageDriver string
}

// Result is the outcome of a successful Create or Restore's manifest read.
type Result struct {
	Path string
	Size int64
}

// Create builds a backup archive: a safe live copy of the SQLite database
// (via `VACUUM INTO`, so it is safe to run against a data directory a
// server is concurrently serving requests from), every file under
// <DataDir>/uploads (skipped cleanly if that directory does not exist), and
// a manifest.json describing the backup. It returns the final archive path
// and size.
func Create(ctx context.Context, opts CreateOptions) (Result, error) {
	now := time.Now().UTC()

	dbCopyPath, cleanup, err := vacuumCopy(ctx, opts.DataDir)
	if err != nil {
		return Result{}, err
	}
	defer cleanup()

	outPath := resolveOutPath(opts.DataDir, opts.Out, now)
	if err := os.MkdirAll(filepath.Dir(outPath), archiveDirPerm); err != nil {
		return Result{}, fmt.Errorf("backup: create output directory: %w", err)
	}

	if err := writeArchive(outPath, dbCopyPath, opts, now); err != nil {
		return Result{}, err
	}

	info, err := os.Stat(outPath)
	if err != nil {
		return Result{}, fmt.Errorf("backup: stat archive: %w", err)
	}
	return Result{Path: outPath, Size: info.Size()}, nil
}

// vacuumCopy opens the store at dataDir and runs `VACUUM INTO` to produce a
// self-contained, consistent snapshot of the database in a scratch temp
// directory, without disturbing the live database file (VACUUM INTO reads
// the source database but never writes to it, so this is safe to run
// concurrently with a server that has the same file open). It returns the
// snapshot's path and a cleanup func that removes the scratch directory;
// the caller must call cleanup once done with the path.
func vacuumCopy(ctx context.Context, dataDir string) (path string, cleanup func(), err error) {
	st, err := store.Open(dataDir)
	if err != nil {
		return "", func() {}, fmt.Errorf("backup: open store: %w", err)
	}
	defer st.Close()

	tmpDir, err := os.MkdirTemp("", "pocketpress-backup-*")
	if err != nil {
		return "", func() {}, fmt.Errorf("backup: create temp directory: %w", err)
	}
	cleanup = func() { os.RemoveAll(tmpDir) }

	tmpDB, err := filepath.Abs(filepath.Join(tmpDir, DBFileName))
	if err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("backup: resolve temp db path: %w", err)
	}

	if _, err := st.DB().ExecContext(ctx, "VACUUM INTO ?", tmpDB); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("backup: vacuum database: %w", err)
	}

	return tmpDB, cleanup, nil
}

// resolveOutPath applies the --out contract documented on CreateOptions.Out.
func resolveOutPath(dataDir, out string, now time.Time) string {
	name := defaultArchiveName(now)
	switch {
	case out == "":
		return filepath.Join(dataDir, BackupsDirName, name)
	case strings.HasSuffix(out, ".zip"):
		return out
	default:
		return filepath.Join(out, name)
	}
}

func defaultArchiveName(now time.Time) string {
	return fmt.Sprintf("pocketpress-backup-%s.zip", now.Format(backupTimestampLayout))
}

// writeArchive writes the zip archive at outPath: the vacuumed database
// copy at dbCopyPath as DBEntry, every file under <DataDir>/uploads as
// UploadsPrefix entries, and a manifest.json built from opts and now.
func writeArchive(outPath, dbCopyPath string, opts CreateOptions, now time.Time) (err error) {
	f, err := os.Create(outPath)
	if err != nil {
		return fmt.Errorf("backup: create archive: %w", err)
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()

	zw := zip.NewWriter(f)
	defer func() {
		if cerr := zw.Close(); err == nil {
			err = cerr
		}
	}()

	if err := addFileToZip(zw, DBEntry, dbCopyPath); err != nil {
		return err
	}

	uploadsRoot := filepath.Join(opts.DataDir, UploadsDirName)
	if err := addUploadsToZip(zw, uploadsRoot); err != nil {
		return err
	}

	manifest := Manifest{
		PocketPressVersion: opts.Version,
		CreatedAt:          now.Format(time.RFC3339),
		StorageDriver:      opts.StorageDriver,
	}
	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("backup: encode manifest: %w", err)
	}
	mw, err := zw.Create(ManifestEntry)
	if err != nil {
		return fmt.Errorf("backup: write manifest entry: %w", err)
	}
	if _, err := mw.Write(manifestBytes); err != nil {
		return fmt.Errorf("backup: write manifest entry: %w", err)
	}

	return nil
}

// addUploadsToZip walks uploadsRoot and adds every regular file under it to
// zw as a UploadsPrefix-prefixed entry, preserving its relative path. A
// missing uploads directory is not an error: local uploads are simply
// skipped.
func addUploadsToZip(zw *zip.Writer, uploadsRoot string) error {
	info, err := os.Stat(uploadsRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("backup: stat uploads directory: %w", err)
	}
	if !info.IsDir() {
		return nil
	}

	return filepath.WalkDir(uploadsRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(uploadsRoot, p)
		if err != nil {
			return fmt.Errorf("backup: relativize %q: %w", p, err)
		}
		return addFileToZip(zw, UploadsPrefix+filepath.ToSlash(rel), p)
	})
}

// addFileToZip copies the file at srcPath into zw as a deflated entry named
// name.
func addFileToZip(zw *zip.Writer, name, srcPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("backup: open %q: %w", srcPath, err)
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return fmt.Errorf("backup: stat %q: %w", srcPath, err)
	}

	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		return fmt.Errorf("backup: build zip header for %q: %w", name, err)
	}
	hdr.Name = name
	hdr.Method = zip.Deflate

	w, err := zw.CreateHeader(hdr)
	if err != nil {
		return fmt.Errorf("backup: create zip entry %q: %w", name, err)
	}
	if _, err := io.Copy(w, src); err != nil {
		return fmt.Errorf("backup: write zip entry %q: %w", name, err)
	}
	return nil
}

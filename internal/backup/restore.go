package backup

import (
	"archive/zip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// ErrExistingData is returned by Restore when the target data directory
// already contains a pocketpress.db and Force was not set.
var ErrExistingData = errors.New("backup: data directory already contains a database (use --force to overwrite)")

// ErrInvalidArchive is returned by Restore when the zip is not a valid
// PocketPress backup archive: manifest.json or pocketpress.db is missing,
// the manifest cannot be parsed, or an entry's path would escape the target
// data directory. Restore performs this validation before touching
// anything on disk.
var ErrInvalidArchive = errors.New("backup: not a valid PocketPress backup archive")

// RestoreOptions configures Restore.
type RestoreOptions struct {
	// DataDir is PP_DATA_DIR to restore into.
	DataDir string
	// ArchivePath is the backup zip file to restore from.
	ArchivePath string
	// Force allows Restore to proceed when DataDir already has a
	// pocketpress.db, moving the existing database and uploads into a
	// pre-restore-<timestamp> directory first.
	Force bool
}

// RestoreResult is the outcome of a successful Restore.
type RestoreResult struct {
	// Manifest is the restored archive's manifest.
	Manifest Manifest
	// UploadFiles is the number of uploaded files restored.
	UploadFiles int
	// PreRestoreDir is the directory the previous database/uploads were
	// moved into, or "" if DataDir had no existing database to preserve.
	PreRestoreDir string
}

// Restore extracts a backup archive's pocketpress.db and uploads/ into
// opts.DataDir.
//
// The archive is fully validated — manifest.json and pocketpress.db must
// both be present and parseable, and every entry is checked for path
// traversal (zip-slip protection: an entry whose cleaned path would escape
// its extraction root is rejected) — before anything on disk is touched. If
// opts.DataDir already has a pocketpress.db, Restore refuses with
// ErrExistingData unless opts.Force is set, in which case the existing
// pocketpress.db (and its -wal/-shm sidecars, if present) and uploads/
// directory are moved into <DataDir>/pre-restore-<timestamp>/ before the
// archive is extracted.
//
// Restore does not itself stop or start the PocketPress server; callers
// should ensure the server is stopped before restoring and restarted
// afterwards, since swapping the database file out from under a running
// server is unsafe (unlike backup creation, which uses VACUUM INTO and is
// safe to run concurrently).
func Restore(opts RestoreOptions) (RestoreResult, error) {
	zr, err := zip.OpenReader(opts.ArchivePath)
	if err != nil {
		return RestoreResult{}, fmt.Errorf("backup: open archive: %w", err)
	}
	defer zr.Close()

	plan, err := validateArchive(&zr.Reader, opts.DataDir)
	if err != nil {
		return RestoreResult{}, err
	}

	existingDBPath := filepath.Join(opts.DataDir, DBFileName)
	preRestoreDir := ""
	if _, statErr := os.Stat(existingDBPath); statErr == nil {
		if !opts.Force {
			return RestoreResult{}, ErrExistingData
		}
		preRestoreDir, err = preserveExisting(opts.DataDir)
		if err != nil {
			return RestoreResult{}, err
		}
	} else if !os.IsNotExist(statErr) {
		return RestoreResult{}, fmt.Errorf("backup: stat %q: %w", existingDBPath, statErr)
	}

	if err := os.MkdirAll(opts.DataDir, archiveDirPerm); err != nil {
		return RestoreResult{}, fmt.Errorf("backup: create data directory: %w", err)
	}

	uploadCount, err := applyRestorePlan(plan)
	if err != nil {
		return RestoreResult{}, err
	}

	return RestoreResult{Manifest: plan.manifest, UploadFiles: uploadCount, PreRestoreDir: preRestoreDir}, nil
}

// restoreEntry pairs a validated, in-archive file with the on-disk path it
// will be extracted to.
type restoreEntry struct {
	file   *zip.File
	target string
}

// restorePlan is the fully-validated result of scanning a backup archive:
// every file this package will write to disk, plus the parsed manifest.
// Restore only starts touching the filesystem once a restorePlan has been
// built successfully.
type restorePlan struct {
	manifest Manifest
	db       restoreEntry
	uploads  []restoreEntry
}

// validateArchive scans every entry in zr, rejecting the archive outright
// (before any extraction) if:
//   - any entry's path would escape its extraction root (zip-slip), or
//   - manifest.json or pocketpress.db is missing or unparseable.
//
// It returns the plan Restore then applies.
func validateArchive(zr *zip.Reader, dataDir string) (restorePlan, error) {
	var plan restorePlan
	var haveManifest, haveDB bool

	dbTarget := filepath.Join(dataDir, DBFileName)
	uploadsRoot := filepath.Join(dataDir, UploadsDirName)

	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}

		// Defense in depth: every entry in the archive, not just the ones
		// this package recognizes and extracts, must resolve to a path
		// that does not escape its nominal root once cleaned. This is what
		// rejects a hand-crafted entry like "../evil" outright, even though
		// such a name would not otherwise match DBEntry or UploadsPrefix.
		if !isSafeZipEntryName(f.Name) {
			return restorePlan{}, fmt.Errorf("%w: entry %q escapes the extraction target", ErrInvalidArchive, f.Name)
		}

		switch {
		case f.Name == ManifestEntry:
			m, err := readManifest(f)
			if err != nil {
				return restorePlan{}, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
			}
			plan.manifest = m
			haveManifest = true

		case f.Name == DBEntry:
			plan.db = restoreEntry{file: f, target: dbTarget}
			haveDB = true

		case strings.HasPrefix(f.Name, UploadsPrefix):
			rel := strings.TrimPrefix(f.Name, UploadsPrefix)
			target, err := safeJoin(uploadsRoot, rel)
			if err != nil {
				return restorePlan{}, fmt.Errorf("%w: %v", ErrInvalidArchive, err)
			}
			plan.uploads = append(plan.uploads, restoreEntry{file: f, target: target})
		}
	}

	if !haveManifest {
		return restorePlan{}, fmt.Errorf("%w: missing %s", ErrInvalidArchive, ManifestEntry)
	}
	if !haveDB {
		return restorePlan{}, fmt.Errorf("%w: missing %s", ErrInvalidArchive, DBEntry)
	}

	return plan, nil
}

// isSafeZipEntryName reports whether a zip entry's name, once cleaned,
// stays a relative path that does not climb above its own root — the
// standard zip-slip guard. Zip entry names always use "/" regardless of
// platform, per the zip spec, so this operates on path (not filepath).
func isSafeZipEntryName(name string) bool {
	if name == "" || path.IsAbs(name) {
		return false
	}
	clean := path.Clean(name)
	return clean != ".." && !strings.HasPrefix(clean, "../")
}

// safeJoin resolves rel (a zip entry path with a recognized prefix already
// stripped, e.g. "photo.png" from "uploads/photo.png") against root,
// rejecting anything that would still land outside root after
// platform-native cleaning. This is a second, belt-and-suspenders check on
// top of isSafeZipEntryName, mirroring internal/media's LocalDriver.resolve.
func safeJoin(root, rel string) (string, error) {
	full := filepath.Join(root, filepath.FromSlash(rel))

	r, err := filepath.Rel(root, full)
	if err != nil || r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entry %q escapes %q", rel, root)
	}
	return full, nil
}

func readManifest(f *zip.File) (Manifest, error) {
	rc, err := f.Open()
	if err != nil {
		return Manifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer rc.Close()

	var m Manifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	return m, nil
}

// applyRestorePlan extracts every file in plan to its validated target
// path, database first. It returns the number of uploaded files extracted.
func applyRestorePlan(plan restorePlan) (int, error) {
	if err := extractEntry(plan.db); err != nil {
		return 0, err
	}
	for _, e := range plan.uploads {
		if err := extractEntry(e); err != nil {
			return 0, err
		}
	}
	return len(plan.uploads), nil
}

func extractEntry(e restoreEntry) error {
	if err := os.MkdirAll(filepath.Dir(e.target), archiveDirPerm); err != nil {
		return fmt.Errorf("backup: create directory for %q: %w", e.target, err)
	}

	rc, err := e.file.Open()
	if err != nil {
		return fmt.Errorf("backup: open archive entry %q: %w", e.file.Name, err)
	}
	defer rc.Close()

	out, err := os.OpenFile(e.target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, archiveFilePerm)
	if err != nil {
		return fmt.Errorf("backup: create %q: %w", e.target, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, rc); err != nil {
		return fmt.Errorf("backup: write %q: %w", e.target, err)
	}
	return nil
}

// preserveExisting moves dataDir's current pocketpress.db (and its -wal/
// -shm sidecars, if present) and uploads/ directory into a fresh
// <dataDir>/pre-restore-<timestamp>/ directory, returning that directory's
// path. It is called only when Restore is about to overwrite existing data
// with --force.
func preserveExisting(dataDir string) (string, error) {
	preDir := filepath.Join(dataDir, fmt.Sprintf("pre-restore-%s", time.Now().UTC().Format(backupTimestampLayout)))
	if err := os.MkdirAll(preDir, archiveDirPerm); err != nil {
		return "", fmt.Errorf("backup: create pre-restore directory: %w", err)
	}

	for _, name := range []string{DBFileName, DBFileName + "-wal", DBFileName + "-shm"} {
		src := filepath.Join(dataDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := os.Rename(src, filepath.Join(preDir, name)); err != nil {
			return "", fmt.Errorf("backup: preserve %q: %w", name, err)
		}
	}

	uploadsSrc := filepath.Join(dataDir, UploadsDirName)
	if info, err := os.Stat(uploadsSrc); err == nil && info.IsDir() {
		if err := os.Rename(uploadsSrc, filepath.Join(preDir, UploadsDirName)); err != nil {
			return "", fmt.Errorf("backup: preserve uploads: %w", err)
		}
	}

	return preDir, nil
}

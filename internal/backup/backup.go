// Package backup implements PocketPress's backup/restore subsystem: it
// builds a zip archive containing a safe live copy of the SQLite database, a
// snapshot of the locally-stored uploads, and a JSON manifest describing the
// backup, and it can list, prune and restore those archives.
//
// Nothing in this package talks HTTP or imports internal/http; it is called
// directly by the cmd/pocketpress backup subcommands (see
// cmd/pocketpress/backup.go) and by the automatic backup Scheduler started
// from app.Serve when PP_BACKUP_AUTO is enabled. S3-backed uploads are
// intentionally not copied into the archive (development plan §12) — the
// manifest simply records which storage driver was configured when the
// backup was taken.
package backup

import "github.com/pocketpress/pocketpress/internal/store"

// Entry names and directories fixed by the backup archive format.
const (
	// ManifestEntry is the manifest.json entry inside a backup archive.
	ManifestEntry = "manifest.json"

	// DBEntry is the SQLite database entry inside a backup archive.
	DBEntry = "pocketpress.db"

	// UploadsPrefix is the zip path prefix under which local uploaded files
	// are stored inside a backup archive. Every entry whose name starts
	// with this prefix is restored under <PP_DATA_DIR>/uploads.
	UploadsPrefix = "uploads/"

	// UploadsDirName is the local uploads directory name under PP_DATA_DIR
	// (mirrors internal/media's local driver root, which is rooted at
	// <PP_DATA_DIR>/uploads by frozen contract).
	UploadsDirName = "uploads"

	// BackupsDirName is the default subdirectory of PP_DATA_DIR that
	// `backup create` writes into and `backup list` reads from.
	BackupsDirName = "backups"

	// DBFileName is the SQLite database file name inside PP_DATA_DIR. It is
	// the same value as store.DBFileName; re-exported here as a named
	// constant so callers of this package do not need to import
	// internal/store just to name the file.
	DBFileName = store.DBFileName
)

// Manifest is the JSON document stored as manifest.json inside every backup
// archive, per development plan §12: the PocketPress version, an RFC3339 UTC
// creation timestamp, and the storage driver ("local" or "s3") that was
// configured when the backup was taken. When the driver is "s3", the
// archive's uploads/ entries (if any) only cover files that happen to have
// been synced locally — S3 objects themselves are not copied into every
// backup.
type Manifest struct {
	PocketPressVersion string `json:"pocketpress_version"`
	CreatedAt          string `json:"created_at"`
	StorageDriver      string `json:"storage_driver"`
}

package backup

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Entry describes one backup archive found by List.
type Entry struct {
	// Name is the archive's base file name.
	Name string
	// Path is the archive's full path.
	Path string
	// Size is the archive's size in bytes.
	Size int64
	// CreatedAt is the backup's creation time: the manifest's created_at
	// when available, otherwise the archive file's modification time.
	CreatedAt time.Time
	// FromManifest reports whether CreatedAt came from the archive's
	// manifest.json (true) or its file modification time (false).
	FromManifest bool
}

// List returns every *.zip file in <dataDir>/backups, newest first. A
// missing backups directory is not an error: it returns an empty slice.
func List(dataDir string) ([]Entry, error) {
	dir := filepath.Join(dataDir, BackupsDirName)

	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("backup: read backups directory: %w", err)
	}

	var entries []Entry
	for _, f := range files {
		if f.IsDir() || filepath.Ext(f.Name()) != ".zip" {
			continue
		}
		path := filepath.Join(dir, f.Name())

		info, err := f.Info()
		if err != nil {
			return nil, fmt.Errorf("backup: stat %q: %w", path, err)
		}

		entry := Entry{Name: f.Name(), Path: path, Size: info.Size(), CreatedAt: info.ModTime()}
		if created, ok := manifestCreatedAt(path); ok {
			entry.CreatedAt = created
			entry.FromManifest = true
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].CreatedAt.After(entries[j].CreatedAt) })
	return entries, nil
}

// manifestCreatedAt best-effort reads created_at out of a backup archive's
// manifest.json. It returns ok=false on any error (missing manifest,
// corrupt zip, unparseable timestamp) so List falls back to the archive
// file's modification time rather than failing the whole listing over one
// unreadable archive.
func manifestCreatedAt(path string) (time.Time, bool) {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return time.Time{}, false
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.Name != ManifestEntry {
			continue
		}
		t, ok := parseManifestFile(f)
		return t, ok
	}
	return time.Time{}, false
}

func parseManifestFile(f *zip.File) (time.Time, bool) {
	rc, err := f.Open()
	if err != nil {
		return time.Time{}, false
	}
	defer rc.Close()

	var m Manifest
	if err := json.NewDecoder(rc).Decode(&m); err != nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, m.CreatedAt)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

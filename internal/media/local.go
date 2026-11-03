package media

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	localDirPerm  = 0o755
	localFilePerm = 0o644

	// uploadsSubdir is the fixed subdirectory of PP_DATA_DIR the local
	// driver roots itself at, per the frozen contract.
	uploadsSubdir = "uploads"
)

// LocalDriver stores media files on local disk, rooted at
// <PP_DATA_DIR>/uploads. It is the default Driver (PP_STORAGE_DRIVER=local).
type LocalDriver struct {
	root string
}

// NewLocalDriver builds a LocalDriver rooted at <dataDir>/uploads, creating
// that directory (and dataDir itself, if missing) up front so a later Save
// never fails solely because the root doesn't exist yet.
func NewLocalDriver(dataDir string) (*LocalDriver, error) {
	root := filepath.Join(dataDir, uploadsSubdir)
	if err := os.MkdirAll(root, localDirPerm); err != nil {
		return nil, fmt.Errorf("create uploads dir %q: %w", root, err)
	}
	return &LocalDriver{root: root}, nil
}

// resolve maps a storage key to an absolute filesystem path rooted at
// d.root, rejecting any key that could escape it: an empty key, an absolute
// path, or any "." /".." path segment. This is the sole traversal guard for
// the local driver: every exported method routes its key through it before
// touching the filesystem. Keys are always generated internally (see
// storageKey in service.go, which only ever combines a year/month and a
// freshly generated UUID), so a traversal attempt here would indicate a bug
// rather than untrusted input — reject it outright rather than silently
// rewriting it to something else.
func (d *LocalDriver) resolve(key string) (string, error) {
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("media: empty storage key")
	}

	slashKey := filepath.ToSlash(key)
	if path.IsAbs(slashKey) {
		return "", fmt.Errorf("media: storage key %q must be relative", key)
	}
	for _, part := range strings.Split(slashKey, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("media: storage key %q contains an invalid path segment", key)
		}
	}

	full := filepath.Join(d.root, filepath.FromSlash(slashKey))

	// Belt-and-suspenders: confirm the resolved path is still inside root
	// even after filepath.Join's own cleaning.
	rel, err := filepath.Rel(d.root, full)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("media: storage key %q escapes the storage root", key)
	}
	return full, nil
}

// Save implements Driver.
func (d *LocalDriver) Save(_ context.Context, key string, r io.Reader, _ int64, _ string) error {
	full, err := d.resolve(key)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(full), localDirPerm); err != nil {
		return fmt.Errorf("create storage directory for %q: %w", key, err)
	}

	f, err := os.OpenFile(full, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, localFilePerm)
	if err != nil {
		return fmt.Errorf("create file for key %q: %w", key, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, r); err != nil {
		return fmt.Errorf("write file for key %q: %w", key, err)
	}
	return nil
}

// Open implements Driver.
func (d *LocalDriver) Open(_ context.Context, key string) (io.ReadCloser, error) {
	full, err := d.resolve(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("open file for key %q: %w", key, err)
	}
	return f, nil
}

// Delete implements Driver.
func (d *LocalDriver) Delete(_ context.Context, key string) error {
	full, err := d.resolve(key)
	if err != nil {
		return err
	}
	if err := os.Remove(full); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete file for key %q: %w", key, err)
	}
	return nil
}

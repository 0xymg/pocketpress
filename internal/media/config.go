package media

import (
	"fmt"
	"strings"
)

// The two supported PP_STORAGE_DRIVER values.
const (
	DriverLocal = "local"
	DriverS3    = "s3"
)

// Config is the resolved configuration NewDriver needs to build a Driver.
// It mirrors the PP_STORAGE_DRIVER / PP_S3_* environment variables
// (internal/app.Config owns parsing those into this shape); this package
// stays independent of internal/app so there is no import-cycle risk.
type Config struct {
	// DataDir roots the local driver's <DataDir>/uploads directory. Unused
	// when StorageDriver is "s3".
	DataDir string

	// StorageDriver selects the driver: "local" (default, including "") or
	// "s3".
	StorageDriver string

	S3Endpoint       string
	S3Region         string
	S3Bucket         string
	S3AccessKey      string
	S3SecretKey      string
	S3ForcePathStyle bool
}

// NewDriver builds the Driver selected by cfg.StorageDriver. An "s3" driver
// with a missing endpoint, bucket or credential fails fast with a clear
// error here instead of constructing a half-configured client that would
// only fail later, on the first upload — the caller (cmd/pocketpress) is
// expected to call this before the server starts accepting requests, per
// the development plan's startup-validation requirement.
func NewDriver(cfg Config) (Driver, error) {
	switch cfg.StorageDriver {
	case "", DriverLocal:
		return NewLocalDriver(cfg.DataDir)
	case DriverS3:
		return NewS3Driver(cfg)
	default:
		return nil, fmt.Errorf("media: unknown PP_STORAGE_DRIVER %q (want %q or %q)", cfg.StorageDriver, DriverLocal, DriverS3)
	}
}

// validateS3Config fails fast when a required PP_S3_* setting is missing,
// naming every missing one in a single error.
func validateS3Config(cfg Config) error {
	var missing []string
	if cfg.S3Endpoint == "" {
		missing = append(missing, "PP_S3_ENDPOINT")
	}
	if cfg.S3Bucket == "" {
		missing = append(missing, "PP_S3_BUCKET")
	}
	if cfg.S3AccessKey == "" {
		missing = append(missing, "PP_S3_ACCESS_KEY")
	}
	if cfg.S3SecretKey == "" {
		missing = append(missing, "PP_S3_SECRET_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("media: PP_STORAGE_DRIVER=s3 requires %s to be set", strings.Join(missing, ", "))
	}
	return nil
}

// storageName reports the "local"/"s3" label a Driver's uploads should
// record in media.storage.
func storageName(d Driver) string {
	if _, ok := d.(*S3Driver); ok {
		return DriverS3
	}
	return DriverLocal
}

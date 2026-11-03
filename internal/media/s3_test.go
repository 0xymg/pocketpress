// s3_test.go covers only S3 driver *construction* and config validation —
// parsing the endpoint, building credentials, rejecting an incomplete
// config. There is no live S3 (or S3-compatible) server in this test
// environment, so nothing here performs a network call (Save/Open/Delete
// against a real bucket are exercised only by internal/media's own client
// library, minio-go, which is out of scope here).
package media_test

import (
	"strings"
	"testing"

	"github.com/pocketpress/pocketpress/internal/media"
)

func completeS3Config() media.Config {
	return media.Config{
		StorageDriver: media.DriverS3,
		S3Endpoint:    "s3.example.com",
		S3Region:      "us-east-1",
		S3Bucket:      "pocketpress-media",
		S3AccessKey:   "AKIAEXAMPLE",
		S3SecretKey:   "supersecret",
	}
}

// TestNewDriverS3CompleteConfigSucceeds confirms a fully-specified s3 config
// builds a driver without error (and without making any network call).
func TestNewDriverS3CompleteConfigSucceeds(t *testing.T) {
	d, err := media.NewDriver(completeS3Config())
	if err != nil {
		t.Fatalf("NewDriver() error = %v", err)
	}
	if _, ok := d.(*media.S3Driver); !ok {
		t.Fatalf("NewDriver() returned %T, want *media.S3Driver", d)
	}
}

// TestNewDriverS3MissingSettingsFailFast confirms every required PP_S3_*
// setting is individually enforced, and that the error names the missing
// one.
func TestNewDriverS3MissingSettingsFailFast(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*media.Config)
		wantErr string
	}{
		{"missing endpoint", func(c *media.Config) { c.S3Endpoint = "" }, "PP_S3_ENDPOINT"},
		{"missing bucket", func(c *media.Config) { c.S3Bucket = "" }, "PP_S3_BUCKET"},
		{"missing access key", func(c *media.Config) { c.S3AccessKey = "" }, "PP_S3_ACCESS_KEY"},
		{"missing secret key", func(c *media.Config) { c.S3SecretKey = "" }, "PP_S3_SECRET_KEY"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := completeS3Config()
			tc.mutate(&cfg)

			_, err := media.NewDriver(cfg)
			if err == nil {
				t.Fatal("NewDriver() error = nil, want a fail-fast config error")
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("NewDriver() error = %q, want it to mention %s", err.Error(), tc.wantErr)
			}

			if _, err := media.NewS3Driver(cfg); err == nil {
				t.Fatal("NewS3Driver() error = nil, want a fail-fast config error")
			}
		})
	}
}

// TestNewDriverS3RegionAndForcePathStyleOptional confirms an s3 config with
// no region and no force-path-style setting still builds successfully (only
// endpoint/bucket/access key/secret key are required).
func TestNewDriverS3RegionAndForcePathStyleOptional(t *testing.T) {
	cfg := completeS3Config()
	cfg.S3Region = ""
	cfg.S3ForcePathStyle = false
	if _, err := media.NewDriver(cfg); err != nil {
		t.Fatalf("NewDriver() without region/force-path-style = %v, want nil", err)
	}
}

// TestNewDriverUnknownStorageDriver confirms a PP_STORAGE_DRIVER value other
// than "local"/""/"s3" is rejected.
func TestNewDriverUnknownStorageDriver(t *testing.T) {
	_, err := media.NewDriver(media.Config{StorageDriver: "dropbox", DataDir: t.TempDir()})
	if err == nil {
		t.Fatal("NewDriver() with unknown storage driver = nil, want an error")
	}
}

// TestNewDriverLocalDefault confirms an empty StorageDriver (the zero value)
// builds a local driver, matching PP_STORAGE_DRIVER's own default.
func TestNewDriverLocalDefault(t *testing.T) {
	d, err := media.NewDriver(media.Config{DataDir: t.TempDir()})
	if err != nil {
		t.Fatalf("NewDriver() error = %v", err)
	}
	if _, ok := d.(*media.LocalDriver); !ok {
		t.Fatalf("NewDriver() with empty StorageDriver returned %T, want *media.LocalDriver", d)
	}
}

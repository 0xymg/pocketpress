package media

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Driver stores media files in an S3-compatible bucket via the MinIO Go
// client, which speaks the S3 API and works against AWS S3 and
// S3-compatible services alike (MinIO, Cloudflare R2, Backblaze B2, ...).
type S3Driver struct {
	client *minio.Client
	bucket string
}

// NewS3Driver builds an S3Driver from cfg. It only constructs and
// configures the client (parses the endpoint, builds static credentials) —
// it never makes a network call, so an unreachable endpoint or a bucket
// that doesn't exist is only discovered on first use (Save/Open/Delete),
// not here. Callers should validate cfg with validateS3Config first (see
// NewDriver), so the only errors possible here come from minio.New itself
// rejecting a malformed endpoint.
func NewS3Driver(cfg Config) (*S3Driver, error) {
	if err := validateS3Config(cfg); err != nil {
		return nil, err
	}

	host, secure := splitEndpoint(cfg.S3Endpoint)

	lookup := minio.BucketLookupAuto
	if cfg.S3ForcePathStyle {
		lookup = minio.BucketLookupPath
	}

	client, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.S3AccessKey, cfg.S3SecretKey, ""),
		Secure:       secure,
		Region:       cfg.S3Region,
		BucketLookup: lookup,
	})
	if err != nil {
		return nil, fmt.Errorf("media: configure s3 client: %w", err)
	}

	return &S3Driver{client: client, bucket: cfg.S3Bucket}, nil
}

// splitEndpoint strips a leading "http://"/"https://" scheme from endpoint
// (the minio client wants a bare host[:port]) and reports whether the
// connection should use TLS. An endpoint without an explicit scheme
// defaults to secure (https), matching typical production S3 usage.
func splitEndpoint(endpoint string) (host string, secure bool) {
	switch {
	case strings.HasPrefix(endpoint, "http://"):
		return strings.TrimPrefix(endpoint, "http://"), false
	case strings.HasPrefix(endpoint, "https://"):
		return strings.TrimPrefix(endpoint, "https://"), true
	default:
		return endpoint, true
	}
}

// Save implements Driver.
func (d *S3Driver) Save(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := d.client.PutObject(ctx, d.bucket, key, r, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("media: s3 put object %q: %w", key, err)
	}
	return nil
}

// Open implements Driver.
func (d *S3Driver) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := d.client.GetObject(ctx, d.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("media: s3 get object %q: %w", key, err)
	}

	// GetObject itself doesn't contact the server; force a Stat so a
	// missing object surfaces as ErrNotFound here instead of on the
	// caller's first Read.
	if _, statErr := obj.Stat(); statErr != nil {
		_ = obj.Close()
		if minio.ToErrorResponse(statErr).Code == "NoSuchKey" {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("media: s3 stat object %q: %w", key, statErr)
	}
	return obj, nil
}

// Delete implements Driver.
func (d *S3Driver) Delete(ctx context.Context, key string) error {
	if err := d.client.RemoveObject(ctx, d.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("media: s3 remove object %q: %w", key, err)
	}
	return nil
}

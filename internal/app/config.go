package app

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds PocketPress runtime configuration loaded from environment
// variables. Only the settings needed for Phase 1 (repository and
// executable foundation) are represented here; later phases will extend
// this struct as new subsystems (auth, storage, webhooks, ...) are added.
type Config struct {
	// Env is the runtime environment, e.g. "development" or "production".
	// It controls the logging format (text vs JSON).
	Env string

	// HTTPAddr is the TCP address the HTTP server listens on.
	HTTPAddr string

	// DataDir is the directory used for SQLite storage and uploaded files.
	DataDir string

	// PublicURL is the externally reachable base URL of this instance, if any.
	// Optional; empty string means it was not configured.
	PublicURL string

	// LogLevel is the minimum slog level to emit, e.g. "debug", "info",
	// "warn" or "error".
	LogLevel string

	// AppSecret is the application secret (PP_APP_SECRET) used to sign
	// preview tokens. When empty, an ephemeral random secret is generated at
	// startup (see app.ResolvePreviewSecret) — fine for development, but
	// preview tokens then do not survive a restart. Production fail-fast on a
	// missing secret arrives in Phase 8.
	AppSecret string

	// EncryptionKey is the application encryption key (PP_ENCRYPTION_KEY) used
	// to encrypt webhook signing secrets at rest (see app.ResolveEncryptionKey).
	// Any non-empty string is accepted (hashed to a 32-byte key). When empty,
	// the key is derived from the resolved app secret instead — fine for
	// development, but webhook secrets then do not survive a restart if the app
	// secret is itself ephemeral. Production fail-fast arrives in Phase 8.
	EncryptionKey string

	// MaxUploadMB caps a single media upload's size, in megabytes (Phase 4).
	MaxUploadMB int

	// StorageDriver selects where uploaded media files are stored: "local"
	// (default) or "s3". See internal/media.
	StorageDriver string

	// S3Endpoint, S3Region, S3Bucket, S3AccessKey and S3SecretKey configure
	// the S3-compatible driver; they are required when StorageDriver is
	// "s3" (see Validate) and ignored otherwise.
	S3Endpoint  string
	S3Region    string
	S3Bucket    string
	S3AccessKey string
	S3SecretKey string

	// S3ForcePathStyle selects path-style bucket addressing
	// (https://host/bucket/key) instead of virtual-hosted-style
	// (https://bucket.host/key), which some S3-compatible services (e.g.
	// MinIO in its default configuration) require.
	S3ForcePathStyle bool

	// CORSOrigins is the static public API CORS allowlist (PP_CORS_ORIGINS,
	// comma-separated). The public API's CORS middleware allows an Origin
	// that exact-matches an entry here OR an entry in the runtime
	// settings.cors_origins list (see internal/http/publicapi); nil means no
	// statically-configured origins, not "allow anything".
	CORSOrigins []string

	// TrustedProxies is the list of reverse-proxy IPs/CIDRs (PP_TRUSTED_PROXIES,
	// comma-separated) whose X-Forwarded-For header PocketPress will trust when
	// determining a request's client IP (used by the login rate limiter). Empty
	// (the default) means X-Forwarded-For is never trusted and the direct peer
	// address is always used — the safe default for a directly-exposed instance.
	TrustedProxies []string
}

// prodSecretMinLen is the minimum length required for PP_APP_SECRET and
// PP_ENCRYPTION_KEY when PP_ENV=production.
const prodSecretMinLen = 32

const (
	envPPEnv              = "PP_ENV"
	envPPHTTPAddr         = "PP_HTTP_ADDR"
	envPPDataDir          = "PP_DATA_DIR"
	envPPPublicURL        = "PP_PUBLIC_URL"
	envPPLogLevel         = "PP_LOG_LEVEL"
	envPPAppSecret        = "PP_APP_SECRET"
	envPPEncryptionKey    = "PP_ENCRYPTION_KEY"
	envPPMaxUploadMB      = "PP_MAX_UPLOAD_MB"
	envPPStorageDriver    = "PP_STORAGE_DRIVER"
	envPPS3Endpoint       = "PP_S3_ENDPOINT"
	envPPS3Region         = "PP_S3_REGION"
	envPPS3Bucket         = "PP_S3_BUCKET"
	envPPS3AccessKey      = "PP_S3_ACCESS_KEY"
	envPPS3SecretKey      = "PP_S3_SECRET_KEY"
	envPPS3ForcePathStyle = "PP_S3_FORCE_PATH_STYLE"
	envPPCORSOrigins      = "PP_CORS_ORIGINS"
	envPPTrustedProxies   = "PP_TRUSTED_PROXIES"

	defaultEnv           = "development"
	defaultHTTPAddr      = "0.0.0.0:8090"
	defaultDataDir       = "./pp_data"
	defaultLogLevel      = "info"
	defaultMaxUploadMB   = 10
	defaultStorageDriver = "local"
	defaultS3ForcePath   = false

	// storageDriverLocal and storageDriverS3 are the only valid
	// StorageDriver values; Validate rejects anything else.
	storageDriverLocal = "local"
	storageDriverS3    = "s3"
)

// LoadConfig builds a Config from environment variables using the provided
// getenv function. It is a pure function (no direct dependency on the
// process environment) so it is easy to unit test; production code should
// call Load, which wires it to os.Getenv.
func LoadConfig(getenv func(string) string) Config {
	return Config{
		Env:              stringOrDefault(getenv(envPPEnv), defaultEnv),
		HTTPAddr:         stringOrDefault(getenv(envPPHTTPAddr), defaultHTTPAddr),
		DataDir:          stringOrDefault(getenv(envPPDataDir), defaultDataDir),
		PublicURL:        getenv(envPPPublicURL),
		LogLevel:         stringOrDefault(getenv(envPPLogLevel), defaultLogLevel),
		AppSecret:        getenv(envPPAppSecret),
		EncryptionKey:    getenv(envPPEncryptionKey),
		MaxUploadMB:      intOrDefault(getenv(envPPMaxUploadMB), defaultMaxUploadMB),
		StorageDriver:    stringOrDefault(getenv(envPPStorageDriver), defaultStorageDriver),
		S3Endpoint:       getenv(envPPS3Endpoint),
		S3Region:         getenv(envPPS3Region),
		S3Bucket:         getenv(envPPS3Bucket),
		S3AccessKey:      getenv(envPPS3AccessKey),
		S3SecretKey:      getenv(envPPS3SecretKey),
		S3ForcePathStyle: boolOrDefault(getenv(envPPS3ForcePathStyle), defaultS3ForcePath),
		CORSOrigins:      parseCORSOrigins(getenv(envPPCORSOrigins)),
		TrustedProxies:   parseCommaList(getenv(envPPTrustedProxies)),
	}
}

// Load builds a Config from the current process environment.
func Load() Config {
	return LoadConfig(os.Getenv)
}

// IsProduction reports whether the config is set to the "production" env.
func (c Config) IsProduction() bool {
	return c.Env == "production"
}

// Validate checks the configuration invariants that must hold before
// PocketPress starts serving requests. Callers (the serve command) run this
// before opening the store or building the media driver, so a misconfigured
// instance fails fast with one clear error instead of surfacing later as a
// broken endpoint at request time.
//
// It enforces, in order:
//   - StorageDriver must be "local" or "s3"; an "s3" driver additionally
//     requires every PP_S3_* connection setting other than region and
//     force-path-style to be set.
//   - PP_PUBLIC_URL, when set, must be an absolute http(s) URL with a host.
//   - When PP_ENV=production, PP_APP_SECRET and PP_ENCRYPTION_KEY must both be
//     set to at least 32 characters (the fail-fast production hardening from
//     the development plan §10/§11). Non-production keeps the warn-and-continue
//     behavior implemented in the serve command, which generates ephemeral
//     secrets instead.
func (c Config) Validate() error {
	if err := c.validateStorage(); err != nil {
		return err
	}
	if err := c.validatePublicURL(); err != nil {
		return err
	}
	if c.IsProduction() {
		if err := c.validateProductionSecrets(); err != nil {
			return err
		}
	}
	return nil
}

// validateStorage enforces the StorageDriver / PP_S3_* invariants.
func (c Config) validateStorage() error {
	switch c.StorageDriver {
	case storageDriverLocal, storageDriverS3:
	default:
		return fmt.Errorf("%s must be %q or %q, got %q", envPPStorageDriver, storageDriverLocal, storageDriverS3, c.StorageDriver)
	}

	if c.StorageDriver != storageDriverS3 {
		return nil
	}

	var missing []string
	if c.S3Endpoint == "" {
		missing = append(missing, envPPS3Endpoint)
	}
	if c.S3Bucket == "" {
		missing = append(missing, envPPS3Bucket)
	}
	if c.S3AccessKey == "" {
		missing = append(missing, envPPS3AccessKey)
	}
	if c.S3SecretKey == "" {
		missing = append(missing, envPPS3SecretKey)
	}
	if len(missing) > 0 {
		return fmt.Errorf("%s=%s requires %s to be set", envPPStorageDriver, storageDriverS3, strings.Join(missing, ", "))
	}
	return nil
}

// validatePublicURL rejects a PP_PUBLIC_URL that is set but is not an
// absolute http(s) URL with a host. An empty value is allowed (the setting is
// optional).
func (c Config) validatePublicURL() error {
	if c.PublicURL == "" {
		return nil
	}
	u, err := url.Parse(c.PublicURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%s must be an absolute http(s) URL, got %q", envPPPublicURL, c.PublicURL)
	}
	return nil
}

// validateProductionSecrets requires the production secrets. It reports every
// problem at once so an operator fixes them in a single pass rather than
// discovering them one restart at a time.
func (c Config) validateProductionSecrets() error {
	var problems []string
	if p := secretProblem(envPPAppSecret, c.AppSecret); p != "" {
		problems = append(problems, p)
	}
	if p := secretProblem(envPPEncryptionKey, c.EncryptionKey); p != "" {
		problems = append(problems, p)
	}
	if len(problems) > 0 {
		return fmt.Errorf("%s=production requires:\n  - %s", envPPEnv, strings.Join(problems, "\n  - "))
	}
	return nil
}

// secretProblem returns a human-readable problem describing why the named
// secret fails the production length requirement, or "" when it is acceptable.
func secretProblem(name, value string) string {
	switch {
	case value == "":
		return fmt.Sprintf("%s must be set (at least %d characters)", name, prodSecretMinLen)
	case len(value) < prodSecretMinLen:
		return fmt.Sprintf("%s must be at least %d characters (got %d)", name, prodSecretMinLen, len(value))
	default:
		return ""
	}
}

func stringOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

// intOrDefault parses value as a positive integer, falling back to
// fallback when value is empty, unparseable or not positive (PP_MAX_UPLOAD_MB
// must be a whole number of megabytes greater than zero).
func intOrDefault(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return fallback
	}
	return n
}

// boolOrDefault parses value with strconv.ParseBool, falling back to
// fallback when value is empty or unparseable.
func boolOrDefault(value string, fallback bool) bool {
	if value == "" {
		return fallback
	}
	b, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return b
}

// parseCORSOrigins splits a PP_CORS_ORIGINS value on commas, trimming
// whitespace and dropping empty entries. An unset/blank value yields nil
// (no statically-configured origins; the runtime settings.cors_origins
// allowlist can still allow requests on its own).
func parseCORSOrigins(value string) []string {
	return parseCommaList(value)
}

// parseCommaList splits value on commas, trimming whitespace and dropping
// empty entries. An unset/blank value yields nil. It backs both
// PP_CORS_ORIGINS and PP_TRUSTED_PROXIES.
func parseCommaList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

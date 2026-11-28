package app

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

// strongSecret is a 40-character value that clears the 32-char production
// minimum for PP_APP_SECRET / PP_ENCRYPTION_KEY.
const strongSecret = "0123456789012345678901234567890123456789"

// prodEnv builds a minimal valid production env map, then applies overrides.
func prodEnv(overrides map[string]string) map[string]string {
	env := map[string]string{
		"PP_ENV":            "production",
		"PP_APP_SECRET":     strongSecret,
		"PP_ENCRYPTION_KEY": strongSecret,
	}
	for k, v := range overrides {
		if v == "" {
			delete(env, k)
			continue
		}
		env[k] = v
	}
	return env
}

// TestConfigValidateTrustedProxies confirms PP_TRUSTED_PROXIES is parsed into
// the comma-separated list (whitespace trimmed, blanks dropped) and defaults to
// nil when unset.
func TestConfigValidateTrustedProxies(t *testing.T) {
	if cfg := LoadConfig(fakeEnv(nil)); cfg.TrustedProxies != nil {
		t.Fatalf("TrustedProxies default = %#v, want nil", cfg.TrustedProxies)
	}
	cfg := LoadConfig(fakeEnv(map[string]string{"PP_TRUSTED_PROXIES": " 10.0.0.1 , 192.168.0.0/16 ,,"}))
	want := []string{"10.0.0.1", "192.168.0.0/16"}
	if !reflect.DeepEqual(cfg.TrustedProxies, want) {
		t.Fatalf("TrustedProxies = %#v, want %#v", cfg.TrustedProxies, want)
	}
}

// TestConfigValidateProductionRequiresSecrets is the production fail-fast
// matrix: with either secret missing or too short, Validate must fail and name
// every offending variable; with both set to >=32 chars it must pass.
func TestConfigValidateProductionRequiresSecrets(t *testing.T) {
	t.Run("both missing lists both", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(prodEnv(map[string]string{
			"PP_APP_SECRET":     "",
			"PP_ENCRYPTION_KEY": "",
		})))
		err := cfg.Validate()
		if err == nil {
			t.Fatal("Validate() with both production secrets missing = nil, want an error")
		}
		if !strings.Contains(err.Error(), "PP_APP_SECRET") || !strings.Contains(err.Error(), "PP_ENCRYPTION_KEY") {
			t.Fatalf("error = %q, want it to name both PP_APP_SECRET and PP_ENCRYPTION_KEY", err.Error())
		}
	})

	t.Run("too short fails", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(prodEnv(map[string]string{
			"PP_APP_SECRET":     "too-short",
			"PP_ENCRYPTION_KEY": strongSecret,
		})))
		err := cfg.Validate()
		if err == nil {
			t.Fatal("Validate() with a short PP_APP_SECRET = nil, want an error")
		}
		if !strings.Contains(err.Error(), "PP_APP_SECRET") {
			t.Fatalf("error = %q, want it to name PP_APP_SECRET", err.Error())
		}
		if strings.Contains(err.Error(), "PP_ENCRYPTION_KEY") {
			t.Fatalf("error = %q, should not name the valid PP_ENCRYPTION_KEY", err.Error())
		}
	})

	t.Run("valid secrets pass", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(prodEnv(nil)))
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() with valid production secrets = %v, want nil", err)
		}
	})
}

// TestConfigValidateDevelopmentSkipsSecretChecks confirms the warn-and-continue
// behavior is preserved outside production: missing secrets are not a Validate
// error in development.
func TestConfigValidateDevelopmentSkipsSecretChecks(t *testing.T) {
	cfg := LoadConfig(fakeEnv(map[string]string{"PP_ENV": "development"}))
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() in development without secrets = %v, want nil", err)
	}
}

// TestConfigValidatePublicURL confirms a set PP_PUBLIC_URL must be an http(s)
// URL with a host, regardless of environment, while an unset value is allowed.
func TestConfigValidatePublicURL(t *testing.T) {
	t.Run("valid https passes", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(prodEnv(map[string]string{"PP_PUBLIC_URL": "https://cms.example.com"})))
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() with a valid PP_PUBLIC_URL = %v, want nil", err)
		}
	})

	t.Run("non-url fails", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(prodEnv(map[string]string{"PP_PUBLIC_URL": "not-a-url"})))
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "PP_PUBLIC_URL") {
			t.Fatalf("Validate() with a bad PP_PUBLIC_URL = %v, want an error naming PP_PUBLIC_URL", err)
		}
	})

	t.Run("ftp scheme fails in development too", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(map[string]string{"PP_PUBLIC_URL": "ftp://example.com"}))
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate() with an ftp PP_PUBLIC_URL = nil, want an error")
		}
	})
}

// fakeEnv builds a getenv function backed by a plain map, for deterministic
// config-loading tests independent of the real process environment.
func fakeEnv(values map[string]string) func(string) string {
	return func(key string) string {
		return values[key]
	}
}

func TestLoadConfigDefaults(t *testing.T) {
	cfg := LoadConfig(fakeEnv(nil))

	want := Config{
		Env:              "development",
		HTTPAddr:         "0.0.0.0:8090",
		DataDir:          "./pp_data",
		PublicURL:        "",
		LogLevel:         "info",
		MaxUploadMB:      10,
		StorageDriver:    "local",
		S3ForcePathStyle: false,
		CORSOrigins:      nil,
		BackupAuto:       true,
		BackupInterval:   24 * time.Hour,
		BackupKeep:       7,
	}

	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("LoadConfig(no env) = %+v, want %+v", cfg, want)
	}

	if cfg.IsProduction() {
		t.Fatalf("IsProduction() = true, want false for default env %q", cfg.Env)
	}
}

func TestLoadConfigOverrides(t *testing.T) {
	env := fakeEnv(map[string]string{
		"PP_ENV":                 "production",
		"PP_HTTP_ADDR":           "127.0.0.1:9090",
		"PP_DATA_DIR":            "/var/lib/pocketpress",
		"PP_PUBLIC_URL":          "https://cms.example.com",
		"PP_LOG_LEVEL":           "debug",
		"PP_MAX_UPLOAD_MB":       "25",
		"PP_STORAGE_DRIVER":      "s3",
		"PP_S3_ENDPOINT":         "s3.example.com",
		"PP_S3_REGION":           "us-east-1",
		"PP_S3_BUCKET":           "pocketpress-media",
		"PP_S3_ACCESS_KEY":       "AKIAEXAMPLE",
		"PP_S3_SECRET_KEY":       "supersecret",
		"PP_S3_FORCE_PATH_STYLE": "true",
		"PP_CORS_ORIGINS":        "https://example.com, https://www.example.com ,,https://other.example.com",
		"PP_APP_SECRET":          "super-secret-app-key",
		"PP_ENCRYPTION_KEY":      "super-secret-encryption-key",
		"PP_BACKUP_AUTO":         "false",
		"PP_BACKUP_INTERVAL":     "12h",
		"PP_BACKUP_KEEP":         "3",
	})

	cfg := LoadConfig(env)

	want := Config{
		Env:              "production",
		HTTPAddr:         "127.0.0.1:9090",
		DataDir:          "/var/lib/pocketpress",
		PublicURL:        "https://cms.example.com",
		LogLevel:         "debug",
		AppSecret:        "super-secret-app-key",
		EncryptionKey:    "super-secret-encryption-key",
		MaxUploadMB:      25,
		StorageDriver:    "s3",
		S3Endpoint:       "s3.example.com",
		S3Region:         "us-east-1",
		S3Bucket:         "pocketpress-media",
		S3AccessKey:      "AKIAEXAMPLE",
		S3SecretKey:      "supersecret",
		S3ForcePathStyle: true,
		CORSOrigins:      []string{"https://example.com", "https://www.example.com", "https://other.example.com"},
		BackupAuto:       false,
		BackupInterval:   12 * time.Hour,
		BackupKeep:       3,
	}

	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("LoadConfig(overrides) = %+v, want %+v", cfg, want)
	}

	if !cfg.IsProduction() {
		t.Fatalf("IsProduction() = false, want true for env %q", cfg.Env)
	}
}

// TestLoadConfigMaxUploadMBFallback confirms PP_MAX_UPLOAD_MB falls back to
// the default of 10 for empty, unparseable and non-positive values.
func TestLoadConfigMaxUploadMBFallback(t *testing.T) {
	for _, v := range []string{"", "not-a-number", "0", "-5"} {
		cfg := LoadConfig(fakeEnv(map[string]string{"PP_MAX_UPLOAD_MB": v}))
		if cfg.MaxUploadMB != 10 {
			t.Errorf("PP_MAX_UPLOAD_MB=%q -> MaxUploadMB = %d, want default 10", v, cfg.MaxUploadMB)
		}
	}
}

// TestLoadConfigS3ForcePathStyleFallback confirms PP_S3_FORCE_PATH_STYLE
// falls back to false for empty/unparseable values but honors valid bool
// spellings.
func TestLoadConfigS3ForcePathStyleFallback(t *testing.T) {
	cases := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"not-a-bool", false},
		{"true", true},
		{"1", true},
		{"false", false},
		{"0", false},
	}
	for _, tc := range cases {
		cfg := LoadConfig(fakeEnv(map[string]string{"PP_S3_FORCE_PATH_STYLE": tc.value}))
		if cfg.S3ForcePathStyle != tc.want {
			t.Errorf("PP_S3_FORCE_PATH_STYLE=%q -> S3ForcePathStyle = %v, want %v", tc.value, cfg.S3ForcePathStyle, tc.want)
		}
	}
}

func TestLoadConfigPartialOverride(t *testing.T) {
	// Only overriding one variable should leave the rest at their defaults.
	env := fakeEnv(map[string]string{
		"PP_LOG_LEVEL": "warn",
	})

	cfg := LoadConfig(env)

	if cfg.LogLevel != "warn" {
		t.Fatalf("LogLevel = %q, want %q", cfg.LogLevel, "warn")
	}
	if cfg.Env != "development" {
		t.Fatalf("Env = %q, want default %q", cfg.Env, "development")
	}
	if cfg.HTTPAddr != "0.0.0.0:8090" {
		t.Fatalf("HTTPAddr = %q, want default %q", cfg.HTTPAddr, "0.0.0.0:8090")
	}
	if cfg.DataDir != "./pp_data" {
		t.Fatalf("DataDir = %q, want default %q", cfg.DataDir, "./pp_data")
	}
}

// TestLoadConfigCORSOriginsFallback confirms PP_CORS_ORIGINS is absent
// (nil, not an empty non-nil slice) for empty/whitespace-only values, and
// that a blank entry between commas is dropped rather than producing an
// empty-string origin.
func TestLoadConfigCORSOriginsFallback(t *testing.T) {
	for _, v := range []string{"", "   ", ",", " , "} {
		cfg := LoadConfig(fakeEnv(map[string]string{"PP_CORS_ORIGINS": v}))
		if cfg.CORSOrigins != nil {
			t.Errorf("PP_CORS_ORIGINS=%q -> CORSOrigins = %#v, want nil", v, cfg.CORSOrigins)
		}
	}
}

// TestConfigValidateLocalDriver confirms the default ("local") storage
// driver never fails validation, regardless of the (irrelevant) S3 fields.
func TestConfigValidateLocalDriver(t *testing.T) {
	cfg := LoadConfig(fakeEnv(nil))
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() with default local driver = %v, want nil", err)
	}
}

// TestConfigValidateUnknownDriver confirms an unrecognized PP_STORAGE_DRIVER
// value fails validation.
func TestConfigValidateUnknownDriver(t *testing.T) {
	cfg := LoadConfig(fakeEnv(map[string]string{"PP_STORAGE_DRIVER": "dropbox"}))
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() with unknown storage driver = nil, want an error")
	}
}

// TestConfigValidateS3RequiresConnectionSettings confirms an s3 driver with
// any of the required PP_S3_* settings missing fails fast, and that
// supplying all of them passes.
func TestConfigValidateS3RequiresConnectionSettings(t *testing.T) {
	full := map[string]string{
		"PP_STORAGE_DRIVER": "s3",
		"PP_S3_ENDPOINT":    "s3.example.com",
		"PP_S3_BUCKET":      "media",
		"PP_S3_ACCESS_KEY":  "key",
		"PP_S3_SECRET_KEY":  "secret",
	}

	t.Run("complete config passes", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(full))
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() with a complete s3 config = %v, want nil", err)
		}
	})

	for _, missingKey := range []string{"PP_S3_ENDPOINT", "PP_S3_BUCKET", "PP_S3_ACCESS_KEY", "PP_S3_SECRET_KEY"} {
		t.Run("missing "+missingKey, func(t *testing.T) {
			env := map[string]string{}
			for k, v := range full {
				if k == missingKey {
					continue
				}
				env[k] = v
			}
			cfg := LoadConfig(fakeEnv(env))
			err := cfg.Validate()
			if err == nil {
				t.Fatalf("Validate() with %s missing = nil, want an error", missingKey)
			}
			if !strings.Contains(err.Error(), missingKey) {
				t.Fatalf("Validate() error = %q, want it to name %s", err.Error(), missingKey)
			}
		})
	}

	// PP_S3_REGION and PP_S3_FORCE_PATH_STYLE are not required.
	t.Run("region and force-path-style are optional", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(full))
		cfg.S3Region = ""
		cfg.S3ForcePathStyle = false
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() without region/force-path-style = %v, want nil", err)
		}
	})
}

// TestLoadConfigBackupDefaultsAndFallbacks covers PP_BACKUP_* loading:
// defaults when unset, overrides when set, and fallbacks for bad values.
func TestLoadConfigBackupDefaultsAndFallbacks(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(nil))
		if !cfg.BackupAuto {
			t.Fatal("BackupAuto default = false, want true")
		}
		if cfg.BackupInterval != 24*time.Hour {
			t.Fatalf("BackupInterval = %s, want 24h", cfg.BackupInterval)
		}
		if cfg.BackupKeep != 7 {
			t.Fatalf("BackupKeep = %d, want 7", cfg.BackupKeep)
		}
	})

	t.Run("disable auto", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(map[string]string{"PP_BACKUP_AUTO": "false"}))
		if cfg.BackupAuto {
			t.Fatal("PP_BACKUP_AUTO=false -> BackupAuto = true, want false")
		}
	})

	for _, v := range []string{"", "nope", "0", "-1h"} {
		cfg := LoadConfig(fakeEnv(map[string]string{"PP_BACKUP_INTERVAL": v}))
		if cfg.BackupInterval != 24*time.Hour {
			t.Errorf("PP_BACKUP_INTERVAL=%q -> %s, want default 24h", v, cfg.BackupInterval)
		}
	}

	for _, v := range []string{"", "nope", "0", "-3"} {
		cfg := LoadConfig(fakeEnv(map[string]string{"PP_BACKUP_KEEP": v}))
		if cfg.BackupKeep != 7 {
			t.Errorf("PP_BACKUP_KEEP=%q -> %d, want default 7", v, cfg.BackupKeep)
		}
	}
}

// TestConfigValidateBackup enforces interval/keep only when auto backups are
// enabled; disabled auto-backup skips those checks.
func TestConfigValidateBackup(t *testing.T) {
	t.Run("enabled with valid settings", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(nil))
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() with default backup settings = %v, want nil", err)
		}
	})

	t.Run("enabled with short interval", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(nil))
		cfg.BackupInterval = 30 * time.Second
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "PP_BACKUP_INTERVAL") {
			t.Fatalf("Validate() with short interval = %v, want an error naming PP_BACKUP_INTERVAL", err)
		}
	})

	t.Run("enabled with keep zero", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(nil))
		cfg.BackupKeep = 0
		err := cfg.Validate()
		if err == nil || !strings.Contains(err.Error(), "PP_BACKUP_KEEP") {
			t.Fatalf("Validate() with keep=0 = %v, want an error naming PP_BACKUP_KEEP", err)
		}
	})

	t.Run("disabled skips interval/keep checks", func(t *testing.T) {
		cfg := LoadConfig(fakeEnv(map[string]string{"PP_BACKUP_AUTO": "false"}))
		cfg.BackupInterval = time.Second
		cfg.BackupKeep = 0
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate() with auto disabled = %v, want nil", err)
		}
	})
}

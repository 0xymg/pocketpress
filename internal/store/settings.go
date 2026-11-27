package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Settings is the single row of the settings table (singleton: id is
// always 1).
type Settings struct {
	SiteName                   string
	SiteURL                    string
	AdminTitle                 string
	Locale                     string
	AvailableLocales           []string // BCP 47 tags the site publishes content in
	Timezone                   string
	DefaultAuthor              string // user ID, "" means unset (NULL in storage)
	CORSOrigins                []string
	PublicAPIEnabled           bool
	PreviewURLTemplate         string
	WebhookUserAgent           string
	RequireReviewBeforePublish bool
}

// settingsID is the fixed primary key of the singleton settings row.
const settingsID = 1

// CreateSettings inserts the singleton settings row. It is a plain
// INSERT, not an upsert: the settings.id PRIMARY KEY CHECK (id = 1)
// constraint means a second call fails with a constraint violation. That
// is intentional — it is what makes "setup cannot run twice" (see the
// development plan's Phase 2 acceptance criteria) hold at the storage
// layer instead of relying only on an application-level check.
func CreateSettings(ctx context.Context, db *sql.DB, s Settings) (Settings, error) {
	if len(s.AvailableLocales) == 0 {
		if s.Locale != "" {
			s.AvailableLocales = []string{s.Locale}
		} else {
			s.AvailableLocales = []string{"en"}
		}
	}
	corsOrigins, err := marshalCORSOrigins(s.CORSOrigins)
	if err != nil {
		return Settings{}, err
	}
	availableLocales, err := marshalStringList(s.AvailableLocales)
	if err != nil {
		return Settings{}, err
	}

	_, err = db.ExecContext(ctx, `
		INSERT INTO settings (
			id, site_name, site_url, admin_title, locale, available_locales, timezone,
			default_author, cors_origins, public_api_enabled,
			preview_url_template, webhook_user_agent, require_review_before_publish
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		settingsID, s.SiteName, s.SiteURL, s.AdminTitle, s.Locale, availableLocales, s.Timezone,
		nullString(s.DefaultAuthor), corsOrigins, boolToInt(s.PublicAPIEnabled),
		nullString(s.PreviewURLTemplate), s.WebhookUserAgent, boolToInt(s.RequireReviewBeforePublish),
	)
	if err != nil {
		return Settings{}, fmt.Errorf("insert settings: %w", err)
	}

	return s, nil
}

// GetSettings returns the singleton settings row. Returns ErrNotFound if
// setup has not created it yet.
func GetSettings(ctx context.Context, db *sql.DB) (Settings, error) {
	row := db.QueryRowContext(ctx, `
		SELECT site_name, site_url, admin_title, locale, available_locales, timezone,
			default_author, cors_origins, public_api_enabled,
			preview_url_template, webhook_user_agent, require_review_before_publish
		FROM settings
		WHERE id = ?`,
		settingsID,
	)

	var (
		s                          Settings
		availableLocales           string
		defaultAuthor              sql.NullString
		corsOrigins                string
		publicAPIEnabled           int
		previewURLTemplate         sql.NullString
		requireReviewBeforePublish int
	)

	err := row.Scan(&s.SiteName, &s.SiteURL, &s.AdminTitle, &s.Locale, &availableLocales, &s.Timezone,
		&defaultAuthor, &corsOrigins, &publicAPIEnabled, &previewURLTemplate, &s.WebhookUserAgent,
		&requireReviewBeforePublish,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return Settings{}, ErrNotFound
	}
	if err != nil {
		return Settings{}, fmt.Errorf("scan settings: %w", err)
	}

	s.DefaultAuthor = stringOrEmpty(defaultAuthor)
	s.PublicAPIEnabled = publicAPIEnabled != 0
	s.PreviewURLTemplate = stringOrEmpty(previewURLTemplate)
	s.RequireReviewBeforePublish = requireReviewBeforePublish != 0

	if s.AvailableLocales, err = unmarshalStringList(availableLocales); err != nil {
		return Settings{}, fmt.Errorf("unmarshal available_locales: %w", err)
	}
	if s.CORSOrigins, err = unmarshalCORSOrigins(corsOrigins); err != nil {
		return Settings{}, err
	}

	return s, nil
}

// UpdateSettings overwrites the singleton settings row. Returns
// ErrNotFound if setup has not created it yet.
func UpdateSettings(ctx context.Context, db *sql.DB, s Settings) error {
	corsOrigins, err := marshalCORSOrigins(s.CORSOrigins)
	if err != nil {
		return err
	}
	availableLocales, err := marshalStringList(s.AvailableLocales)
	if err != nil {
		return err
	}

	res, err := db.ExecContext(ctx, `
		UPDATE settings SET
			site_name = ?, site_url = ?, admin_title = ?, locale = ?, available_locales = ?, timezone = ?,
			default_author = ?, cors_origins = ?, public_api_enabled = ?,
			preview_url_template = ?, webhook_user_agent = ?, require_review_before_publish = ?
		WHERE id = ?`,
		s.SiteName, s.SiteURL, s.AdminTitle, s.Locale, availableLocales, s.Timezone,
		nullString(s.DefaultAuthor), corsOrigins, boolToInt(s.PublicAPIEnabled),
		nullString(s.PreviewURLTemplate), s.WebhookUserAgent, boolToInt(s.RequireReviewBeforePublish),
		settingsID,
	)
	if err != nil {
		return fmt.Errorf("update settings: %w", err)
	}
	return requireRowsAffected(res)
}

// IsInitialized reports whether PocketPress has completed first-run setup:
// at least one active owner exists, and the singleton settings row exists.
// Both must hold, since setup creates them together (see the development
// plan's "After setup" sequence).
func IsInitialized(ctx context.Context, db *sql.DB) (bool, error) {
	var ownerExists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM users WHERE role = ? AND active = 1)`, RoleOwner,
	).Scan(&ownerExists); err != nil {
		return false, fmt.Errorf("check owner exists: %w", err)
	}
	if !ownerExists {
		return false, nil
	}

	var settingsExists bool
	if err := db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM settings WHERE id = ?)`, settingsID,
	).Scan(&settingsExists); err != nil {
		return false, fmt.Errorf("check settings exists: %w", err)
	}

	return settingsExists, nil
}

func marshalCORSOrigins(origins []string) (string, error) {
	return marshalStringList(origins)
}

func unmarshalCORSOrigins(raw string) ([]string, error) {
	origins, err := unmarshalStringList(raw)
	if err != nil {
		return nil, fmt.Errorf("unmarshal cors_origins: %w", err)
	}
	return origins, nil
}

func marshalStringList(items []string) (string, error) {
	if items == nil {
		items = []string{}
	}
	b, err := json.Marshal(items)
	if err != nil {
		return "", fmt.Errorf("marshal string list: %w", err)
	}
	return string(b), nil
}

func unmarshalStringList(raw string) ([]string, error) {
	var items []string
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, err
	}
	if items == nil {
		items = []string{}
	}
	return items, nil
}

package store_test

import (
	"context"
	"errors"
	"testing"

	"github.com/pocketpress/pocketpress/internal/store"
)

func testSettings() store.Settings {
	return store.Settings{
		SiteName:         "My Blog",
		SiteURL:          "https://example.com",
		AdminTitle:       "PocketPress",
		Locale:           "en",
		Timezone:         "UTC",
		CORSOrigins:      []string{"https://example.com"},
		PublicAPIEnabled: true,
		WebhookUserAgent: "PocketPress/0.1.0",
	}
}

// TestSettingsSingletonCreateGetSecondCreateFails confirms CreateSettings
// stores the singleton row, GetSettings reads it back faithfully
// (including the JSON-encoded CORSOrigins slice), and that a second
// CreateSettings call fails — enforcing "setup cannot run twice" at the
// storage layer via the settings.id PRIMARY KEY CHECK (id = 1) constraint.
func TestSettingsSingletonCreateGetSecondCreateFails(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	in := testSettings()

	if _, err := store.CreateSettings(ctx, st.DB(), in); err != nil {
		t.Fatalf("CreateSettings() error = %v", err)
	}

	got, err := store.GetSettings(ctx, st.DB())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	if got.SiteName != in.SiteName || got.SiteURL != in.SiteURL {
		t.Fatalf("GetSettings() SiteName/SiteURL = %q/%q, want %q/%q",
			got.SiteName, got.SiteURL, in.SiteName, in.SiteURL)
	}
	if len(got.CORSOrigins) != 1 || got.CORSOrigins[0] != "https://example.com" {
		t.Fatalf("GetSettings().CORSOrigins = %v, want [https://example.com]", got.CORSOrigins)
	}
	if !got.PublicAPIEnabled {
		t.Fatal("GetSettings().PublicAPIEnabled = false, want true")
	}

	if _, err := store.CreateSettings(ctx, st.DB(), in); err == nil {
		t.Fatal("CreateSettings() (second call) error = nil, want a constraint error")
	}
}

// TestGetSettingsNotFoundBeforeSetup confirms GetSettings returns
// ErrNotFound before setup has ever created the singleton row.
func TestGetSettingsNotFoundBeforeSetup(t *testing.T) {
	st := openTestStore(t)

	_, err := store.GetSettings(context.Background(), st.DB())
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("GetSettings() error = %v, want ErrNotFound", err)
	}
}

// TestUpdateSettings confirms UpdateSettings overwrites the singleton row
// in place.
func TestUpdateSettings(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	in := testSettings()

	if _, err := store.CreateSettings(ctx, st.DB(), in); err != nil {
		t.Fatalf("CreateSettings() error = %v", err)
	}

	in.SiteName = "Renamed Blog"
	if err := store.UpdateSettings(ctx, st.DB(), in); err != nil {
		t.Fatalf("UpdateSettings() error = %v", err)
	}

	got, err := store.GetSettings(ctx, st.DB())
	if err != nil {
		t.Fatalf("GetSettings() error = %v", err)
	}
	if got.SiteName != "Renamed Blog" {
		t.Fatalf("GetSettings().SiteName = %q, want %q", got.SiteName, "Renamed Blog")
	}
}

// TestUpdateSettingsNotFound confirms UpdateSettings returns ErrNotFound
// rather than silently doing nothing when setup hasn't run.
func TestUpdateSettingsNotFound(t *testing.T) {
	st := openTestStore(t)

	err := store.UpdateSettings(context.Background(), st.DB(), testSettings())
	if !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("UpdateSettings() error = %v, want ErrNotFound", err)
	}
}

// TestIsInitialized walks the full setup-state progression: false on a
// fresh database, still false with only an owner or only settings, true
// once both exist, and false again if the owner is later deactivated.
func TestIsInitialized(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	initialized, err := store.IsInitialized(ctx, st.DB())
	if err != nil {
		t.Fatalf("IsInitialized() error = %v", err)
	}
	if initialized {
		t.Fatal("IsInitialized() = true on a fresh database, want false")
	}

	owner, err := store.CreateUser(ctx, st.DB(), store.NewUser{
		Email: "owner@example.com", PasswordHash: "hash", Name: "Owner", Role: store.RoleOwner,
	})
	if err != nil {
		t.Fatalf("CreateUser() error = %v", err)
	}

	initialized, err = store.IsInitialized(ctx, st.DB())
	if err != nil {
		t.Fatalf("IsInitialized() error = %v", err)
	}
	if initialized {
		t.Fatal("IsInitialized() = true with an owner but no settings, want false")
	}

	if _, err := store.CreateSettings(ctx, st.DB(), testSettings()); err != nil {
		t.Fatalf("CreateSettings() error = %v", err)
	}

	initialized, err = store.IsInitialized(ctx, st.DB())
	if err != nil {
		t.Fatalf("IsInitialized() error = %v", err)
	}
	if !initialized {
		t.Fatal("IsInitialized() = false with an active owner + settings, want true")
	}

	if err := store.SetUserActive(ctx, st.DB(), owner.ID, false); err != nil {
		t.Fatalf("SetUserActive(false) error = %v", err)
	}
	initialized, err = store.IsInitialized(ctx, st.DB())
	if err != nil {
		t.Fatalf("IsInitialized() error = %v", err)
	}
	if initialized {
		t.Fatal("IsInitialized() = true with only an inactive owner, want false")
	}
}

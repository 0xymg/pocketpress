package adminapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/pocketpress/pocketpress/internal/content"
	"github.com/pocketpress/pocketpress/internal/http/middleware"
	"github.com/pocketpress/pocketpress/internal/store"
)

// settingsDTO is the frozen settings shape. default_author and
// preview_url_template are null (not "") when unset; cors_origins is always
// an array.
type settingsDTO struct {
	SiteName                   string   `json:"site_name"`
	SiteURL                    string   `json:"site_url"`
	AdminTitle                 string   `json:"admin_title"`
	Locale                     string   `json:"locale"`
	AvailableLocales           []string `json:"available_locales"`
	Timezone                   string   `json:"timezone"`
	DefaultAuthor              *string  `json:"default_author"`
	CORSOrigins                []string `json:"cors_origins"`
	PublicAPIEnabled           bool     `json:"public_api_enabled"`
	PreviewURLTemplate         *string  `json:"preview_url_template"`
	WebhookUserAgent           string   `json:"webhook_user_agent"`
	RequireReviewBeforePublish bool     `json:"require_review_before_publish"`
}

func toSettingsDTO(s store.Settings) settingsDTO {
	origins := s.CORSOrigins
	if origins == nil {
		origins = []string{}
	}
	locales := s.AvailableLocales
	if locales == nil {
		locales = []string{}
	}
	return settingsDTO{
		SiteName:                   s.SiteName,
		SiteURL:                    s.SiteURL,
		AdminTitle:                 s.AdminTitle,
		Locale:                     s.Locale,
		AvailableLocales:           locales,
		Timezone:                   s.Timezone,
		DefaultAuthor:              stringPtrOrNil(s.DefaultAuthor),
		CORSOrigins:                origins,
		PublicAPIEnabled:           s.PublicAPIEnabled,
		PreviewURLTemplate:         stringPtrOrNil(s.PreviewURLTemplate),
		WebhookUserAgent:           s.WebhookUserAgent,
		RequireReviewBeforePublish: s.RequireReviewBeforePublish,
	}
}

func (h *Handler) writeSettings(w http.ResponseWriter, status int, s store.Settings) {
	middleware.WriteJSON(w, status, map[string]settingsDTO{"settings": toSettingsDTO(s)})
}

// getSettings (GET /settings) returns the singleton settings row. Any
// authenticated user may read it (editors need available_locales for the
// post editor); PATCH remains owner-only.
func (h *Handler) getSettings(w http.ResponseWriter, r *http.Request) {
	s, err := store.GetSettings(r.Context(), h.db)
	if errors.Is(err, store.ErrNotFound) {
		middleware.WriteError(w, http.StatusNotFound, "not_found", "Settings not found.")
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.writeSettings(w, http.StatusOK, s)
}

// updateSettings (PATCH /settings, owner only) applies a partial update.
// Every field is optional; only provided keys change. Presence is detected
// from the raw JSON object so a field can be cleared (default_author /
// preview_url_template accept null) distinctly from being omitted.
func (h *Handler) updateSettings(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		middleware.WriteError(w, http.StatusBadRequest, "bad_request", "Request body is not valid JSON.")
		return
	}

	s, err := store.GetSettings(r.Context(), h.db)
	if errors.Is(err, store.ErrNotFound) {
		middleware.WriteError(w, http.StatusNotFound, "not_found", "Settings not found.")
		return
	}
	if err != nil {
		h.serverError(w, err)
		return
	}

	fields := map[string]string{}

	if v, ok := raw["site_name"]; ok {
		if name, bad := decodeString(v); bad {
			fields["site_name"] = "Site name must be a string."
		} else if strings.TrimSpace(name) == "" {
			fields["site_name"] = "Site name is required."
		} else {
			s.SiteName = strings.TrimSpace(name)
		}
	}

	if v, ok := raw["site_url"]; ok {
		if u, bad := decodeString(v); bad {
			fields["site_url"] = "Site URL must be a string."
		} else if !isValidHTTPURL(strings.TrimSpace(u)) {
			fields["site_url"] = "Site URL must be a valid http(s) URL."
		} else {
			s.SiteURL = strings.TrimSpace(u)
		}
	}

	if v, ok := raw["admin_title"]; ok {
		if title, bad := decodeString(v); bad {
			fields["admin_title"] = "Admin title must be a string."
		} else {
			s.AdminTitle = strings.TrimSpace(title)
		}
	}

	if v, ok := raw["locale"]; ok {
		if locale, bad := decodeString(v); bad {
			fields["locale"] = "Locale must be a string."
		} else {
			normalized, valid := normalizeSettingsLocale(locale)
			if !valid {
				fields["locale"] = "Locale must be a valid language tag (e.g. en, tr, pt-br)."
			} else {
				s.Locale = normalized
				s.AvailableLocales = ensureLocaleListed(s.AvailableLocales, normalized)
			}
		}
	}

	if v, ok := raw["available_locales"]; ok {
		var locales []string
		if err := json.Unmarshal(v, &locales); err != nil {
			fields["available_locales"] = "Available locales must be an array of strings."
		} else {
			normalized, ferr := normalizeAvailableLocales(locales)
			if ferr != "" {
				fields["available_locales"] = ferr
			} else {
				s.AvailableLocales = normalized
				if s.Locale != "" && !localeListed(normalized, s.Locale) {
					fields["available_locales"] = "Available locales must include the site default locale."
				}
			}
		}
	}

	if v, ok := raw["timezone"]; ok {
		if tz, bad := decodeString(v); bad {
			fields["timezone"] = "Timezone must be a string."
		} else if !isValidTimezone(strings.TrimSpace(tz)) {
			fields["timezone"] = "Timezone must be a valid IANA timezone."
		} else {
			s.Timezone = strings.TrimSpace(tz)
		}
	}

	if v, ok := raw["default_author"]; ok {
		h.applyDefaultAuthor(r, v, &s, fields)
	}

	if v, ok := raw["cors_origins"]; ok {
		var origins []string
		if err := json.Unmarshal(v, &origins); err != nil {
			fields["cors_origins"] = "CORS origins must be an array of strings."
		} else if bad := invalidCORSOrigin(origins); bad != "" {
			fields["cors_origins"] = "Each CORS origin must be of the form scheme://host[:port] with no path or query (got " + bad + ")."
		} else {
			s.CORSOrigins = origins
		}
	}

	if v, ok := raw["public_api_enabled"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			fields["public_api_enabled"] = "Public API enabled must be a boolean."
		} else {
			s.PublicAPIEnabled = b
		}
	}

	if v, ok := raw["require_review_before_publish"]; ok {
		var b bool
		if err := json.Unmarshal(v, &b); err != nil {
			fields["require_review_before_publish"] = "Require review before publish must be a boolean."
		} else {
			s.RequireReviewBeforePublish = b
		}
	}

	if v, ok := raw["preview_url_template"]; ok {
		applyPreviewURLTemplate(v, &s, fields)
	}

	if v, ok := raw["webhook_user_agent"]; ok {
		if ua, bad := decodeString(v); bad {
			fields["webhook_user_agent"] = "Webhook user agent must be a string."
		} else {
			s.WebhookUserAgent = strings.TrimSpace(ua)
		}
	}

	if len(fields) > 0 {
		h.validationError(w, fields)
		return
	}

	if err := store.UpdateSettings(r.Context(), h.db, s); err != nil {
		h.serverError(w, err)
		return
	}
	updated, err := store.GetSettings(r.Context(), h.db)
	if err != nil {
		h.serverError(w, err)
		return
	}
	h.writeSettings(w, http.StatusOK, updated)
}

// applyDefaultAuthor validates and applies the default_author field: null (or
// "") clears it; any other value must name an existing active user.
func (h *Handler) applyDefaultAuthor(r *http.Request, v json.RawMessage, s *store.Settings, fields map[string]string) {
	var idPtr *string
	if err := json.Unmarshal(v, &idPtr); err != nil {
		fields["default_author"] = "Default author must be a user id or null."
		return
	}
	if idPtr == nil || strings.TrimSpace(*idPtr) == "" {
		s.DefaultAuthor = ""
		return
	}
	id := strings.TrimSpace(*idPtr)
	u, err := store.GetUserByID(r.Context(), h.db, id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !u.Active) {
		fields["default_author"] = "Default author must be an existing active user."
		return
	}
	if err != nil {
		// A real lookup failure — surface it as a field error rather than
		// silently accepting; the caller sees validation_failed.
		fields["default_author"] = "Default author could not be validated."
		return
	}
	s.DefaultAuthor = id
}

// applyPreviewURLTemplate validates and applies preview_url_template: null (or
// "") clears it; a non-empty value must be an http(s) URL containing {token}.
func applyPreviewURLTemplate(v json.RawMessage, s *store.Settings, fields map[string]string) {
	var tplPtr *string
	if err := json.Unmarshal(v, &tplPtr); err != nil {
		fields["preview_url_template"] = "Preview URL template must be a string or null."
		return
	}
	if tplPtr == nil {
		s.PreviewURLTemplate = ""
		return
	}
	tpl := strings.TrimSpace(*tplPtr)
	if tpl == "" {
		s.PreviewURLTemplate = ""
		return
	}
	if !isValidHTTPURL(tpl) || !strings.Contains(tpl, "{token}") {
		fields["preview_url_template"] = "Preview URL template must be a valid http(s) URL containing {token}."
		return
	}
	s.PreviewURLTemplate = tpl
}

// decodeString unmarshals a raw JSON value into a string, reporting bad=true
// when it is not a JSON string.
func decodeString(v json.RawMessage) (s string, bad bool) {
	if err := json.Unmarshal(v, &s); err != nil {
		return "", true
	}
	return s, false
}

// stringPtrOrNil returns nil for "" and a pointer to s otherwise — how the
// optional settings fields serialize as JSON null vs. a string.
func stringPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func normalizeSettingsLocale(raw string) (string, bool) {
	return content.NormalizeLocale(raw)
}

func normalizeAvailableLocales(raw []string) ([]string, string) {
	if len(raw) == 0 {
		return nil, "At least one available locale is required."
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		normalized, ok := content.NormalizeLocale(item)
		if !ok {
			return nil, "Each available locale must be a valid language tag (e.g. en, tr, pt-br)."
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil, "At least one available locale is required."
	}
	return out, ""
}

func localeListed(list []string, locale string) bool {
	return content.LocaleInList(locale, list)
}

func ensureLocaleListed(list []string, locale string) []string {
	return content.EnsureLocaleInList(list, locale)
}

// invalidCORSOrigin returns the first entry of origins that is not a bare
// scheme://host[:port] origin (no path, query, fragment or userinfo), or ""
// when they are all valid — the same shape a browser sends in the Origin
// header, which the CORS allowlist compares against exactly.
func invalidCORSOrigin(origins []string) string {
	for _, o := range origins {
		u, err := url.Parse(o)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") ||
			u.Host == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return o
		}
	}
	return ""
}

package content

import "github.com/pocketpress/pocketpress/internal/store"

// Post is a post aggregate: the stored row plus the related records the
// admin API serializes alongside it. The service assembles it; the HTTP
// layer maps it to the frozen POST_SUMMARY / POST_FULL JSON shapes.
//
// CoverImage/OGImage are the hydrated media rows referenced by
// Row.CoverImage/Row.OGImage (a media id), or nil when unset. hydrate also
// leaves them nil (rather than failing the whole load) if a referenced
// media row no longer exists — it shouldn't happen, since the media
// service's delete guard refuses to remove media still referenced by a
// post, but a nil embedded object is a safer failure mode here than a 500.
type Post struct {
	Row          store.Post
	Author       Author
	Categories   []store.Category
	Tags         []store.Tag
	CoverImage   *store.Media
	OGImage      *store.Media
	Translations []store.TranslationRef // siblings in the same translation_group
}

// CategoryInput is the create body for a category. Slug is a pointer so the
// service can tell "field absent/blank" (auto-generate from Name) from a
// provided value (normalize + uniqueness-check).
type CategoryInput struct {
	Name        string
	Slug        *string
	Description string
}

// CategoryPatch is the partial-update body for a category. Each nil field is
// left unchanged.
type CategoryPatch struct {
	Name        *string
	Slug        *string
	Description *string
}

// TagInput / TagPatch mirror the category variants; a tag has no
// description.
type TagInput struct {
	Name string
	Slug *string
}

type TagPatch struct {
	Name *string
	Slug *string
}

// PostCreateInput is the resolved create body for a post. Slug nil/blank
// auto-generates from Title; AuthorID "" defaults to CurrentUserID; Status
// "" defaults to draft. CurrentUserID is the authenticated user, used as the
// revision editor and the default author.
//
// ContentJSON is the serialized Lexical editor state, stored verbatim after a
// JSON-validity check (empty → "{}"). ContentHTML is the editor's client-side
// HTML export; it is sanitized server-side and the plain-text body is derived
// from the sanitized HTML.
type PostCreateInput struct {
	Title          string
	Slug           *string
	Excerpt        string
	ContentJSON    string
	ContentHTML    string
	CoverImage     string // media id; "" = no cover
	Status         string
	AuthorID       string
	CategoryIDs    []string
	TagIDs         []string
	Featured       bool
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	OGImage        string // media id; "" = no OG image
	Locale         string // blank → site default locale
	TranslateOf    string // optional post id to join as a translation of
	Metadata       string // JSON object; blank → "{}"
	CurrentUserID  string
}

// PostUpdateInput is the partial-update body for a post. A nil pointer means
// "field omitted, leave unchanged"; a non-nil pointer applies the value
// (including replacing the category/tag sets, where a non-nil empty slice
// clears them). CurrentUserID is the authenticated user (revision editor).
//
// ContentJSON and ContentHTML are independent partial fields: supplying either
// re-derives the stored plain-text body from the (re-)sanitized HTML; omitting
// both leaves the stored content untouched.
//
// CoverImage/OGImage follow the same *string pattern already used by Slug
// and Description elsewhere in this file: nil leaves the field unchanged; a
// non-nil pointer to a media id sets it (after validating the id exists);
// a non-nil pointer to "" clears it. There is no separate "clear" flag —
// sending an empty string is the documented way to clear either field.
type PostUpdateInput struct {
	Title          *string
	Slug           *string
	Excerpt        *string
	ContentJSON    *string
	ContentHTML    *string
	CoverImage     *string
	Status         *string
	AuthorID       *string
	CategoryIDs    *[]string
	TagIDs         *[]string
	Featured       *bool
	SEOTitle       *string
	SEODescription *string
	CanonicalURL   *string
	OGImage        *string
	Locale         *string
	// TranslateOf: nil = leave group unchanged; pointer to "" = unlink;
	// pointer to a post id = join that post's translation group.
	TranslateOf *string
	// Metadata: nil = leave unchanged; non-nil raw JSON object replaces
	// the stored object (use "{}" to clear).
	Metadata      *string
	CurrentUserID string
}

// Author is the public user projection exposed by the authors endpoint and
// embedded in post summaries: identity and profile fields only — never
// email, role, or password material.
type Author struct {
	ID       string
	Name     string
	Slug     string
	Bio      string
	Website  string
	Location string
	// AvatarURL is a relative /api/files/... path when the user has an
	// avatar media id set; empty when unset or the media row is missing.
	AvatarURL string
}

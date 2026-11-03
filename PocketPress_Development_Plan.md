# PocketPress — Development Plan

## 0. Product decision

**PocketPress is a tiny, self-hosted blog backend for product developers.**

Core promise:

> You built the product. Don’t build the blog backend too.

PocketPress is not a website builder and not a generic CMS. It gives an existing product website:

- a clean writing dashboard,
- posts, categories and tags,
- media uploads,
- draft, scheduled and published states,
- SEO metadata,
- a stable content API,
- preview links,
- webhooks for rebuilding the frontend.

### Primary user

- Indie hackers
- Solo founders
- SaaS developers
- Small product teams
- Freelancers adding blogs to client websites

### Primary use case

A developer has already built a website using Next.js, Astro, Nuxt, SvelteKit or another frontend framework. They deploy PocketPress, create an editor account, connect the API and hand the writing dashboard to whoever will publish content.

---

## 1. MVP scope

### Included in v0.1

- Single instance = single blog
- First-run setup wizard
- Owner and editor authentication
- Posts
- Categories
- Tags
- Cover images and media uploads
- Rich-text editor
- Draft, scheduled, published and archived statuses
- Slugs
- Excerpts
- SEO title and description
- Canonical URL
- Open Graph image
- Public REST API
- Preview tokens
- Publish/update/delete webhooks
- Local file storage
- Optional S3-compatible file storage
- CORS configuration
- Docker image
- Single executable release
- Backup command
- OpenAPI specification

### Explicitly excluded from v0.1

- Themes
- Plugins
- Website rendering
- Page builder
- Generic content model builder
- Custom collections
- Newsletter delivery
- Memberships
- Comments
- E-commerce
- Forms
- GraphQL
- Multi-site management
- Collaborative real-time editing
- Localization
- Analytics
- Search
- JavaScript SDK

Do not add excluded features while implementing the MVP.

---

## 2. Technical strategy

### Recommended foundation

Build PocketPress as a fully standalone Go application, with no embedded framework underneath it.

> An earlier draft embedded PocketBase; this was dropped in favor of a standalone implementation.

Use the standard library `net/http` with Go 1.22+ `ServeMux` routing patterns and a small set of custom middleware. Use `spf13/cobra` for the CLI. Persist data in SQLite via `modernc.org/sqlite` (a pure Go driver, so the binary stays CGO-free and easy to cross-compile). Apply schema changes with plain SQL migration files embedded in the binary and run by a small custom migration runner.

This foundation exists because:

- **No framework lock-in.** The application owns its full request lifecycle instead of adapting around a third-party framework's conventions.
- **Full control of the public API surface.** `/api/v1` is the only contract that exists; there is no internal framework API to accidentally expose or work around.
- **Minimal dependency tree.** Fewer dependencies mean fewer supply-chain and upgrade risks, and a codebase that is easier to read end to end.
- **Single static binary.** A CGO-free stack cross-compiles cleanly to a single executable for every target platform.

PocketPress must define its own stable domain layer and `/api/v1` contract so that internal implementation details (storage engine, session mechanism, migration tooling) can change later without breaking frontend integrations.

### Stack

#### Backend

- Go
- Standard library `net/http` with Go 1.22+ `ServeMux` patterns
- SQLite via `modernc.org/sqlite`
- Embedded SQL migrations with a custom migration runner
- `spf13/cobra` CLI
- `slog` structured logging
- HMAC-signed webhooks

#### Admin application

- Svelte 5 (runes)
- SvelteKit 2 with static adapter in SPA mode
- TypeScript
- Vite
- Tailwind CSS v4
- shadcn-svelte component library
- Lexical rich-text editor (framework-agnostic core with Svelte bindings, e.g. svelte-lexical)
- lightweight fetch-based data layer (TanStack Svelte Query optional later)
- form handling with Svelte 5 runes (form library optional later)

#### Content storage format

Store all three representations:

- `content_json`: canonical editor document (serialized Lexical editor state)
- `content_html`: server-sanitized HTML for immediate rendering
- `content_text`: plain text derived from the document for excerpts and future search

Do not use MDX. It creates framework coupling and introduces executable-content risks.

### Deployment model

PocketPress should run as:

```bash
pocketpress serve
```

or:

```bash
docker run -p 8090:8090 -v pocketpress_data:/app/data pocketpress/pocketpress
```

The compiled admin application must be embedded into the Go executable.

---

## 3. Architecture principles

1. **Opinionated over configurable**  
   Posts, categories and tags exist by default. The user does not design a schema.

2. **Public API independence**  
   External applications only integrate with `/api/v1`; the internal storage schema is never exposed directly.

3. **One instance, one blog**  
   Avoid tenants, workspaces and site switching in the MVP.

4. **Writer-friendly admin**  
   The dashboard should feel like a writing tool, not a database console.

5. **Safe defaults**  
   Published content is readable publicly; drafts and previews always require authorization.

6. **Portable deployment**  
   Local SQLite and files should be enough for a basic installation. S3-compatible storage is optional.

7. **Stable content output**  
   API responses must be versioned and documented.

8. **No hidden frontend assumptions**  
   The API should work equally well with Next.js, Astro, Nuxt, SvelteKit and mobile apps.

---

## 4. Repository structure

```text
pocketpress/
├── cmd/
│   └── pocketpress/
│       └── main.go
├── internal/
│   ├── app/
│   │   ├── app.go
│   │   ├── config.go
│   │   └── lifecycle.go
│   ├── auth/
│   ├── content/
│   │   ├── models.go
│   │   ├── repository.go
│   │   ├── service.go
│   │   ├── slug.go
│   │   ├── sanitizer.go
│   │   └── scheduler.go
│   ├── http/
│   │   ├── router.go
│   │   ├── middleware/
│   │   ├── publicapi/
│   │   └── adminapi/
│   ├── media/
│   ├── preview/
│   ├── webhook/
│   ├── backup/
│   └── observability/
├── migrations/
├── admin/
│   ├── src/
│   │   ├── app/
│   │   ├── components/
│   │   ├── features/
│   │   │   ├── auth/
│   │   │   ├── posts/
│   │   │   ├── categories/
│   │   │   ├── tags/
│   │   │   ├── media/
│   │   │   ├── settings/
│   │   │   └── webhooks/
│   │   ├── lib/
│   │   └── routes/
│   ├── package.json
│   └── vite.config.ts
├── api/
│   └── openapi.yaml
├── e2e/
├── scripts/
├── Dockerfile
├── docker-compose.yml
├── Makefile
├── go.mod
├── README.md
└── LICENSE
```

Keep domain logic out of HTTP handlers. Handlers should validate input, call a service and serialize a response.

---

## 5. Data model

Use migrations for every table and column. Do not create production schema manually against the live database.

### `users`

| Field | Type | Notes |
|---|---|---|
| `id` | text/uuid | Primary key |
| `email` | text | Unique, required |
| `password_hash` | text | argon2id hash, never store raw password |
| `name` | text | Required |
| `role` | text | `owner`, `editor` |
| `avatar` | text | Optional path to stored avatar file |
| `active` | boolean | Default true |
| `last_login_at` | datetime | Optional |
| `created_at` | datetime | Set by application |
| `updated_at` | datetime | Set by application |

Permissions:

- Owner: all content and settings
- Editor: posts, categories, tags and media
- Editor cannot manage users, API settings, storage or webhooks

### `sessions`

| Field | Type | Notes |
|---|---|---|
| `id` | text/uuid | Primary key |
| `token_hash` | text | Hash of the session token; never store the raw token |
| `user` | relation | Required relation to `users` |
| `created_at` | datetime | Set by application |
| `expires_at` | datetime | Required |

Sessions back the server-side, secure HTTP-only cookie auth described in section 2. Expired sessions are rejected and periodically purged.

### `schema_migrations`

Tracks which embedded SQL migrations have been applied (migration id/version and applied timestamp), so the migration runner can determine pending migrations on `migrate up`/`migrate down`. Managed entirely by the migration runner; application code does not write to it directly.

### `posts`

| Field | Type | Notes |
|---|---|---|
| `title` | text | Required, max 200 |
| `slug` | text | Required, unique, normalized |
| `excerpt` | text | Optional, max 500 |
| `content_json` | JSON | Canonical document (serialized Lexical editor state) |
| `content_html` | editor/text | Sanitized server-side |
| `content_text` | text | Derived plain text |
| `cover_image` | file | Optional |
| `status` | select | `draft`, `scheduled`, `published`, `archived` |
| `author` | relation | Required relation to `users` |
| `categories` | relation[] | Relations to `categories` |
| `tags` | relation[] | Relations to `tags` |
| `featured` | boolean | Default false |
| `seo_title` | text | Optional, max 70 |
| `seo_description` | text | Optional, max 170 |
| `canonical_url` | URL | Optional |
| `og_image` | file | Optional |
| `scheduled_for` | date | Required when scheduled |
| `published_at` | date | Set when first published |
| `created` | system date | Set by application |
| `updated` | system date | Set by application |

Rules:

- Public endpoints only return `published` posts with `published_at <= now`.
- A slug cannot be empty.
- A slug conflict returns a validation error; never silently append random values during editing.
- Publishing sets `published_at` if it is empty.
- Returning a published post to draft does not erase its original `published_at`.
- Scheduled posts must have a future `scheduled_for` value.

### `categories`

| Field | Type | Notes |
|---|---|---|
| `name` | text | Required |
| `slug` | text | Unique |
| `description` | text | Optional |

### `tags`

| Field | Type | Notes |
|---|---|---|
| `name` | text | Required |
| `slug` | text | Unique |

### `post_revisions`

| Field | Type | Notes |
|---|---|---|
| `post` | relation | Required |
| `title` | text | Snapshot |
| `excerpt` | text | Snapshot |
| `content_json` | JSON | Snapshot (serialized Lexical editor state) |
| `content_html` | text | Snapshot |
| `editor` | relation | User who created revision |
| `created` | system date | Timestamp |

Revision policy:

- Create a revision when a manually saved post differs from the latest revision.
- Do not create a revision for every keystroke.
- Retain the latest 50 revisions per post in v0.1.

### `settings`

Singleton table (single row).

| Field | Type | Notes |
|---|---|---|
| `site_name` | text | Required |
| `site_url` | URL | Required |
| `admin_title` | text | Defaults to PocketPress |
| `locale` | text | Default `en` |
| `timezone` | text | IANA timezone |
| `default_author` | relation | Optional |
| `cors_origins` | JSON/string[] | Explicit allowlist |
| `public_api_enabled` | boolean | Default true |
| `preview_url_template` | text | Optional |
| `webhook_user_agent` | text | Default PocketPress version |

### `api_keys`

| Field | Type | Notes |
|---|---|---|
| `name` | text | Required |
| `prefix` | text | Display identifier |
| `token_hash` | text | Never store raw token |
| `scopes` | JSON/string[] | `content:read`, `preview:read` |
| `expires_at` | date | Optional |
| `last_used_at` | date | Optional |
| `revoked_at` | date | Optional |

The full key is shown only once at creation.

### `webhooks`

| Field | Type | Notes |
|---|---|---|
| `name` | text | Required |
| `url` | URL | HTTPS required outside development |
| `secret_encrypted` | text | Encrypt at rest |
| `events` | JSON/string[] | Selected events |
| `active` | boolean | Default true |

Supported v0.1 events:

- `post.published`
- `post.updated`
- `post.unpublished`
- `post.deleted`

### `webhook_deliveries`

| Field | Type | Notes |
|---|---|---|
| `webhook` | relation | Required |
| `event` | text | Required |
| `event_id` | text | Unique delivery event ID |
| `payload` | JSON | Delivery body |
| `attempt` | number | Starts at 1 |
| `status` | select | `pending`, `success`, `failed` |
| `response_code` | number | Optional |
| `response_body` | text | Truncated |
| `next_attempt_at` | date | Optional |
| `delivered_at` | date | Optional |

---

## 6. Public API contract

Base path:

```text
/api/v1
```

Return JSON using a stable envelope.

### List posts

```http
GET /api/v1/posts
```

Supported query parameters:

- `page`, default 1
- `per_page`, default 20, max 100
- `category`
- `tag`
- `featured`
- `author`
- `sort`, allowed: `published_at`, `-published_at`, `title`, `-title`

Response:

```json
{
  "data": [
    {
      "id": "post_id",
      "title": "Introducing PocketPress",
      "slug": "introducing-pocketpress",
      "excerpt": "A tiny blog backend for your product.",
      "content_html": "<p>...</p>",
      "cover_image": {
        "url": "https://cms.example.com/api/files/...",
        "width": null,
        "height": null,
        "alt": "PocketPress dashboard"
      },
      "author": {
        "id": "user_id",
        "name": "Yunus"
      },
      "categories": [],
      "tags": [],
      "featured": false,
      "seo": {
        "title": null,
        "description": null,
        "canonical_url": null,
        "og_image_url": null
      },
      "published_at": "2026-08-02T12:00:00Z",
      "updated_at": "2026-08-02T12:00:00Z"
    }
  ],
  "meta": {
    "page": 1,
    "per_page": 20,
    "total_items": 1,
    "total_pages": 1
  }
}
```

### Get post by slug

```http
GET /api/v1/posts/{slug}
```

- Return `404` for drafts, scheduled posts not yet published and archived posts.
- Return an `ETag` header.
- Honor `If-None-Match` and return `304` when unchanged.

### Categories

```http
GET /api/v1/categories
GET /api/v1/categories/{slug}
GET /api/v1/categories/{slug}/posts
```

### Tags

```http
GET /api/v1/tags
GET /api/v1/tags/{slug}
GET /api/v1/tags/{slug}/posts
```

### Health endpoint

```http
GET /healthz
```

Response:

```json
{
  "status": "ok",
  "version": "0.1.0"
}
```

### API error format

```json
{
  "error": {
    "code": "post_not_found",
    "message": "The requested post was not found.",
    "details": null
  }
}
```

Do not expose database errors, collection names or stack traces.

---

## 7. Preview flow

Admin endpoint:

```http
POST /api/admin/v1/posts/{id}/preview-token
```

Response:

```json
{
  "token": "signed-short-lived-token",
  "expires_at": "2026-08-02T12:15:00Z",
  "preview_url": "https://product.example.com/api/preview?token=..."
}
```

Public preview endpoint:

```http
GET /api/v1/preview/posts/{slug}?token=...
```

Requirements:

- Token lifetime defaults to 15 minutes.
- Token is signed using an application secret.
- Token includes post ID, intended action and expiry.
- A token for one post cannot retrieve another post.
- Preview responses use `Cache-Control: no-store`.

---

## 8. Webhook design

Webhook request:

```http
POST https://product.example.com/api/pocketpress-webhook
Content-Type: application/json
User-Agent: PocketPress/0.1.0
X-PocketPress-Event: post.published
X-PocketPress-Delivery: evt_unique_id
X-PocketPress-Timestamp: 1785686400
X-PocketPress-Signature: sha256=...
```

Payload:

```json
{
  "id": "evt_unique_id",
  "event": "post.published",
  "created_at": "2026-08-02T12:00:00Z",
  "data": {
    "post": {
      "id": "post_id",
      "slug": "introducing-pocketpress",
      "status": "published",
      "published_at": "2026-08-02T12:00:00Z"
    }
  }
}
```

Signature:

```text
HMAC-SHA256(secret, timestamp + "." + raw_request_body)
```

Retry schedule:

1. Immediate
2. 1 minute
3. 5 minutes
4. 30 minutes
5. 2 hours

After the fifth failed attempt, mark the delivery failed. The admin can manually retry it.

Webhook delivery must happen asynchronously and must never block saving or publishing a post.

---

## 9. Admin application

### Routes

```text
/setup
/login
/
/posts
/posts/new
/posts/:id
/categories
/tags
/media
/settings/general
/settings/users
/settings/api
/settings/webhooks
```

### First-run setup

When no owner exists, redirect every admin request to `/setup`.

Setup fields:

- Site name
- Site URL
- Owner name
- Owner email
- Password
- Timezone

After setup:

- Create the owner
- Create singleton settings
- Mark installation initialized
- Sign the owner in
- Redirect to `/posts/new`

Setup must be permanently disabled after initialization unless the database is reset.

### Post list

Features:

- Search by title
- Filter by status
- Filter by category
- Sort by updated date
- Pagination
- Create post button
- Status badge
- Scheduled date display
- Quick duplicate
- Delete with confirmation

### Editor screen

Layout:

- Main writing area
- Right sidebar for metadata
- Sticky top action bar

Main fields:

- Title
- Rich-text content
- Excerpt

Sidebar fields:

- Status
- Publish/schedule controls
- Slug
- Author
- Categories
- Tags
- Cover image
- Featured
- SEO fields
- Canonical URL
- Open Graph image

Actions:

- Save draft
- Publish
- Schedule
- Unpublish
- Archive
- Preview
- Delete

Editor behavior:

- Generate slug from title only when slug is empty.
- Never overwrite a manually edited slug.
- Autosave locally in the browser for crash protection.
- Server save remains explicit in v0.1.
- Warn before navigating away with unsaved changes.
- Show last saved timestamp.
- Sanitize generated HTML on the server.

### Dashboard

Keep the dashboard intentionally small:

- Draft count
- Scheduled count
- Published count
- Recently edited posts
- Quick “New post” action

Do not build analytics charts in v0.1.

---

## 10. Security requirements

- Never store raw API keys.
- Encrypt webhook secrets at rest using an application encryption key.
- Require a strong application secret in production.
- Restrict admin APIs to authenticated users.
- Enforce role permissions server-side, not only in the UI.
- Sanitize all HTML server-side using an allowlist.
- Reject dangerous URLs such as `javascript:`.
- Apply request body size limits.
- Apply upload size and MIME type limits.
- Default allowed images: JPEG, PNG, WebP, GIF and SVG only if sanitized; otherwise exclude SVG in v0.1.
- Use secure cookie settings when served over HTTPS.
- Add login rate limiting.
- Add webhook URL validation and block localhost/private-network targets in production to reduce SSRF risk.
- Do not return draft content through public list or detail endpoints.
- Preview tokens must be short-lived and scoped to one post.
- CORS must use an explicit allowlist; do not use credentials with wildcard origins.
- Add security headers for the admin application.
- Do not log passwords, tokens, raw API keys or webhook secrets.

---

## 11. Configuration

Support environment variables and CLI flags. CLI flags take precedence.

Suggested environment variables:

```text
PP_ENV=production
PP_HTTP_ADDR=0.0.0.0:8090
PP_DATA_DIR=/app/data
PP_PUBLIC_URL=https://cms.example.com
PP_APP_SECRET=change-me
PP_ENCRYPTION_KEY=change-me
PP_TRUSTED_PROXIES=
PP_MAX_UPLOAD_MB=10
PP_CORS_ORIGINS=https://example.com,https://www.example.com
PP_LOG_LEVEL=info
PP_STORAGE_DRIVER=local
PP_S3_ENDPOINT=
PP_S3_REGION=
PP_S3_BUCKET=
PP_S3_ACCESS_KEY=
PP_S3_SECRET_KEY=
PP_S3_FORCE_PATH_STYLE=false
```

Startup validation must fail fast when required production secrets are missing.

---

## 12. CLI commands

```bash
pocketpress serve
pocketpress version
pocketpress migrate up
pocketpress migrate down
pocketpress backup create
pocketpress backup list
pocketpress backup restore <file>
pocketpress user create
pocketpress user reset-password
```

Minimum required for v0.1:

- `serve`
- `version`
- `backup create`
- `backup restore`
- `user reset-password`

Backups should include:

- SQLite database
- Local uploaded files
- Metadata manifest with PocketPress version and timestamp

S3 objects do not need to be copied into every backup, but the backup manifest must record the configured storage mode.

---

## 13. Development phases

## Phase 1 — Repository and executable foundation

Tasks:

- Initialize Go module.
- Build the stdlib HTTP server foundation: router (Go 1.22+ `ServeMux` patterns), custom middleware chain and graceful shutdown.
- Create `cmd/pocketpress/main.go`.
- Add configuration loading.
- Add structured logger.
- Register `/healthz`.
- Create Svelte 5/SvelteKit 2 admin project with static adapter in SPA mode.
- Build and embed the admin static output into the Go binary.
- Add Makefile commands for development, test and build.
- Add multi-stage Dockerfile.
- Add basic CI.

Acceptance criteria:

- `make dev` runs backend and admin development servers.
- `make build` produces one executable.
- The executable serves the embedded admin application.
- `/healthz` returns status and version.
- Docker image starts successfully with a persistent data volume.
- Backend and frontend tests can run in CI.

## Phase 2 — Schema, setup and authentication

Tasks:

- Write embedded SQL migrations for all v0.1 tables and build the migration runner (`migrate up`/`migrate down`).
- Implement initialization-state detection.
- Build first-run setup API and UI.
- Implement custom session auth: argon2id password hashing, server-side sessions stored in SQLite, secure HTTP-only cookies.
- Build login and logout.
- Implement owner/editor roles.
- Protect admin routes.
- Add current-user endpoint.
- Add owner user management.

Acceptance criteria:

- A clean installation redirects to setup.
- Setup creates exactly one owner and settings record.
- Setup cannot run twice.
- Owner can log in and log out.
- Editor cannot access user or system settings.
- Unauthenticated admin API requests return `401`.
- Forbidden actions return `403`.

## Phase 3 — Categories, tags and basic posts

Tasks:

- Implement category CRUD.
- Implement tag CRUD.
- Implement post CRUD service.
- Implement slug normalization and uniqueness.
- Build posts list.
- Build new/edit post screens.
- Add draft publishing states.
- Add revision creation on manual save.

Acceptance criteria:

- Owner/editor can create, edit and delete categories and tags.
- Owner/editor can create and save a draft.
- Slugs are normalized and unique.
- Manual slug edits are preserved.
- Each changed manual save creates a revision.
- Unsaved-change navigation warning works.
- Server validation errors appear next to relevant fields.

## Phase 4 — Rich-text editor and media

Tasks:

- Integrate rich-text editor.
- Store JSON document.
- Generate plain text.
- Generate and sanitize HTML server-side.
- Implement image upload.
- Implement media browser.
- Add cover and OG image selection.
- Support local storage.
- Add optional S3-compatible configuration.

Acceptance criteria:

- Writer can format headings, paragraphs, lists, links, quotes, code and images.
- Saved content round-trips without corruption.
- Public HTML contains no disallowed tags or attributes.
- Uploaded files persist after restart.
- File type and size restrictions are enforced.
- The same media UI works with local and S3-compatible storage.

## Phase 5 — Public content API

Tasks:

- Implement `/api/v1/posts`.
- Implement `/api/v1/posts/{slug}`.
- Implement category and tag endpoints.
- Add pagination and filters.
- Add stable response serializers.
- Add cache headers, ETags and `304` support.
- Add CORS allowlist.
- Write OpenAPI specification.
- Add API integration tests.

Acceptance criteria:

- Only published content is returned.
- Drafts and future scheduled content return `404` publicly.
- Filters and pagination behave according to the API spec.
- ETag requests return `304` when appropriate.
- Responses do not leak internal storage fields.
- OpenAPI spec matches actual behavior.

## Phase 6 — Publishing, scheduling and preview

Tasks:

- Implement publish and unpublish operations.
- Implement scheduled publishing job.
- Implement archive operation.
- Implement preview token generation.
- Implement public preview endpoint.
- Add configurable preview URL template.
- Add UI for publish, schedule, unpublish and preview.

Acceptance criteria:

- Publishing sets `published_at` once.
- Scheduled posts become public at the intended time.
- Scheduled publishing survives process restarts.
- Preview works for drafts.
- Expired or altered preview tokens are rejected.
- Preview responses are not cached.

## Phase 7 — Webhooks

Tasks:

- Implement webhook CRUD.
- Implement event creation.
- Implement HMAC signing.
- Implement async delivery worker.
- Implement retry schedule.
- Store delivery history.
- Add manual retry.
- Add “Send test webhook.”
- Add SSRF protections.

Acceptance criteria:

- Publishing a post creates a delivery without delaying the publish request.
- Receiver can verify the signature.
- Failed deliveries retry according to policy.
- Delivery history shows request state and response code.
- Secret is only shown when created or regenerated.
- Private-network URLs are rejected in production.

## Phase 8 — Backup, hardening and dogfooding

Tasks:

- Implement backup creation and restore.
- Add production startup validation.
- Add login rate limiting.
- Add security headers.
- Add request and upload limits.
- Add graceful shutdown.
- Add database integrity checks.
- Add end-to-end tests.
- Deploy separate PocketPress instances for Worklyn and one other product.
- Document installation and frontend integration.

Acceptance criteria:

- A backup can restore a fresh instance with posts and local media intact.
- Process shuts down cleanly.
- End-to-end test covers setup → login → create → publish → public API → webhook.
- Worklyn retrieves and renders its posts from PocketPress.
- A second product can be connected without source-code changes to PocketPress.
- README contains Docker and binary installation instructions.

---

## 14. Testing strategy

### Backend unit tests

- Slug normalization
- Slug conflict handling
- Publishing state transitions
- Scheduling validation
- Preview token signing and expiration
- Webhook signature generation
- Retry timing
- HTML sanitization
- Permission checks

### Backend integration tests

Use a temporary data directory and temporary SQLite database.

Cover:

- Migrations from empty database
- Setup flow
- Authentication
- CRUD
- Public API visibility rules
- Scheduled publication
- Webhook delivery
- Backup and restore

### Frontend tests

- Login form
- Setup wizard
- Post validation
- Status controls
- Unsaved-change guard
- Category/tag selectors
- Permission-driven navigation

### End-to-end test

Use Playwright.

Golden path:

1. Start clean instance.
2. Complete setup.
3. Create category and tag.
4. Create a post with image.
5. Publish it.
6. Fetch it from `/api/v1/posts/{slug}`.
7. Confirm webhook delivery.
8. Edit and save it.
9. Confirm revision exists.
10. Unpublish it.
11. Confirm public endpoint returns `404`.

---

## 15. Definition of MVP complete

PocketPress v0.1 is complete only when a new user can:

1. Start it using one Docker command or one executable.
2. Complete setup without opening a database console.
3. Log into a clean admin application.
4. Write, format and save a post.
5. Upload and reuse an image.
6. Add categories and tags.
7. Publish immediately or schedule publication.
8. Preview a draft on an external product site.
9. Retrieve the published post through a documented API.
10. Trigger a signed webhook that rebuilds the external website.
11. Back up and restore the installation.

Anything beyond this list belongs to a later release unless it fixes security, data loss or severe usability problems.

---

## 16. Post-MVP roadmap

### v0.2

- RSS/Atom feed endpoint
- Sitemap endpoint
- JavaScript/TypeScript SDK
- Import from Markdown
- Import from Ghost/WordPress export
- Full-text search
- Image dimensions and transformations
- Webhook filters
- API key management for private content APIs

### v0.3

- Multiple authors with richer profiles
- Localization
- Content scheduling calendar
- Draft review workflow
- Content duplication templates
- Per-post custom metadata object

### Later, only after demand

- Multi-site management
- Managed PocketPress Cloud
- Team billing
- Automatic backups
- Managed custom domains
- CDN image delivery

Do not build plugin, theme or generic collection systems unless the entire product direction changes.

---

## 17. Instructions for Claude Code

Use this as the master prompt at the root of a new repository:

```text
You are implementing PocketPress, a tiny self-hosted blog backend for product developers.

Read PocketPress_Development_Plan.md completely before modifying files.

Product rule:
PocketPress is not a website builder and not a generic CMS. It provides a writing dashboard and a stable headless content API for an existing product website.

Architecture rule:
Build PocketPress as a fully standalone Go application: stdlib net/http with Go 1.22+ ServeMux patterns, SQLite via modernc.org/sqlite, embedded SQL migrations with a custom runner, and a custom session-based auth layer. Do not introduce an embedded framework. Do not expose internal storage schema as PocketPress's external API. All external integrations must use the custom versioned /api/v1 API.

Working rules:
1. Implement only the requested phase.
2. Do not add features listed under “Explicitly excluded from v0.1”.
3. Use SQL migrations for every schema change.
4. Keep domain logic out of HTTP handlers and UI components.
5. Enforce permissions and validation on the server.
6. Add tests for each implemented behavior.
7. Never store raw API keys or webhook secrets.
8. Do not expose stack traces, internal collection names or raw database errors.
9. Keep the application runnable after every phase.
10. Prefer clear, boring and maintainable code over abstractions without a current use.
11. Do not silently change an existing API response contract.
12. Update the OpenAPI spec whenever public API behavior changes.
13. Run formatting, tests and builds before declaring a phase complete.

At the start of each phase:
- inspect the existing repository,
- summarize the implementation approach,
- list the files you expect to create or modify,
- identify any conflict with the development plan.

At the end of each phase, report:
- what was implemented,
- changed files,
- commands to run it,
- tests added and their results,
- unresolved risks or deviations,
- the next phase, but do not implement it yet.

Begin with Phase 1 only: Repository and executable foundation.
```

### Prompt for each next phase

After reviewing the previous phase, use:

```text
Implement Phase N from PocketPress_Development_Plan.md.

Before coding, inspect the current repository and verify that the previous phase's acceptance criteria are still satisfied. Do not implement later phases. Preserve existing public contracts, add migrations for schema changes, add or update tests, and finish by running the complete relevant test and build commands.
```

---

## 18. Product guardrail

Whenever a new feature is proposed, ask:

> Does this help a developer add and operate a blog for an existing product?

- If yes, consider it.
- If it helps build the website itself, reject it.
- If it turns PocketPress into a generic database CMS, reject it.
- If it mainly serves newsletters, commerce, community or memberships, reject it.

The advantage of PocketPress is not having every feature. Its advantage is that the blog backend is already decided, ready and small.

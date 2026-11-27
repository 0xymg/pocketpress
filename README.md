# PocketPress

**You built the product. Don't build the blog backend too.**

PocketPress is a tiny, self-hosted blog backend for product developers. It gives an existing product website a clean writing dashboard and a stable content API. PocketPress is not a website builder and not a generic CMS.

## Features

- Single executable or Docker image (no framework, no external services required)
- First-run setup wizard
- Owner and editor authentication (session-based, no tokens)
- Posts, categories, and tags with Lexical rich-text editor
- Draft, scheduled, published, and archived states
- Cover images and media uploads (local or S3-compatible storage)
- SEO metadata (title, description, canonical URL, Open Graph image)
- Public REST API at `/api/v1` with pagination, filtering, caching, and ETags
- RSS 2.0 and Atom 1.0 feeds (`/api/v1/feed.rss`, `/api/v1/feed.atom`)
- Preview tokens for sharing drafts
- Webhooks for rebuilding your frontend (post.published, post.updated, post.unpublished, post.deleted)
- Backups (database + local files)
- OpenAPI specification

## Quick start

### Docker

```bash
docker compose up
```

Creates a PocketPress instance at http://localhost:8090. Data persists in a named volume.

Or use `docker run`:

```bash
docker run -p 8090:8090 -v pocketpress_data:/app/data pocketpress/pocketpress
```

### Single binary

Build:

```bash
make build
./pocketpress serve
```

Open http://localhost:8090 in your browser. The setup wizard will guide you through initialization.

## Configuration

All configuration is via environment variables (prefix `PP_`). CLI flags take precedence.

| Variable | Default | Description |
|---|---|---|
| `PP_ENV` | `development` | Runtime environment: `development` or `production` |
| `PP_HTTP_ADDR` | `0.0.0.0:8090` | TCP address to listen on |
| `PP_DATA_DIR` | `./pp_data` | Directory for SQLite database and local file uploads |
| `PP_PUBLIC_URL` | (empty) | Externally-reachable base URL (optional; used for preview URL template) |
| `PP_LOG_LEVEL` | `info` | Minimum log level: `debug`, `info`, `warn`, `error` |
| `PP_APP_SECRET` | (empty) | Secret for signing preview tokens (auto-generated in development) |
| `PP_ENCRYPTION_KEY` | (empty) | Secret for encrypting webhook signing secrets at rest (auto-generated in development) |
| `PP_MAX_UPLOAD_MB` | `10` | Maximum size per file upload, in megabytes |
| `PP_STORAGE_DRIVER` | `local` | Storage backend: `local` or `s3` |
| `PP_S3_ENDPOINT` | (empty) | S3-compatible endpoint URL (required if driver is `s3`) |
| `PP_S3_REGION` | (empty) | S3 region (optional) |
| `PP_S3_BUCKET` | (empty) | S3 bucket name (required if driver is `s3`) |
| `PP_S3_ACCESS_KEY` | (empty) | S3 access key (required if driver is `s3`) |
| `PP_S3_SECRET_KEY` | (empty) | S3 secret key (required if driver is `s3`) |
| `PP_S3_FORCE_PATH_STYLE` | `false` | Use path-style S3 addressing (required for MinIO) |
| `PP_CORS_ORIGINS` | (empty) | CORS allowlist (comma-separated) |
| `PP_TRUSTED_PROXIES` | (empty) | Reverse proxy IPs to trust for X-Forwarded-For (comma-separated) |

**Production requirement:** When `PP_ENV=production`, both `PP_APP_SECRET` and `PP_ENCRYPTION_KEY` must be set to at least 32 characters, or the server refuses to start.

## CLI

```bash
pocketpress serve              # Start the HTTP server
pocketpress version            # Print version and exit
pocketpress migrate up         # Apply pending migrations
pocketpress migrate down       # Rollback the latest migration
pocketpress backup create      # Create a backup (database + local files)
pocketpress backup list        # List available backups
pocketpress backup restore     # Restore from a backup file
pocketpress user create        # Create or update a user (interactive)
pocketpress user reset-password # Reset a user's password (interactive)
```

## Content API

The public content API is at `/api/v1`. See `api/openapi.yaml` for the complete specification.

**Base example:**

```bash
curl https://cms.example.com/api/v1/posts
curl https://cms.example.com/api/v1/feed.rss
curl https://cms.example.com/api/v1/feed.atom
```

**Response structure:**

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
        "url": "/api/files/...",
        "width": null,
        "height": null,
        "alt": "..."
      },
      "author": {
        "id": "user_id",
        "name": "Author Name"
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
    "total_items": 100,
    "total_pages": 5
  }
}
```

**Get a single post by slug:**

```bash
curl https://cms.example.com/api/v1/posts/introducing-pocketpress
```

Responses include an `ETag` header. Subsequent requests with `If-None-Match` return `304 Not Modified` if unchanged. List endpoints cache for 60 seconds; detail endpoints must revalidate.

**Learn more:** See `docs/integration.md` for frontend integration examples (Next.js, Astro, SvelteKit) and the full OpenAPI spec in `api/openapi.yaml`.

## Webhooks

PocketPress can send webhooks when posts are published, updated, unpublished, or deleted.

**Signature verification:**

Every webhook request includes an `X-PocketPress-Signature` header computed as:

```
HMAC-SHA256(secret, timestamp + "." + raw_body)
```

Extract the timestamp from `X-PocketPress-Timestamp`, concatenate it with a dot and the raw request body, then verify the signature using your webhook secret. See `docs/integration.md` for a code example.

**Events:**

- `post.published` — A post transitioned to published
- `post.updated` — A published post was edited
- `post.unpublished` — A post was reverted to draft
- `post.deleted` — A post was permanently deleted

**Retry schedule:**

Failed deliveries retry on this schedule:

1. Immediate
2. 1 minute
3. 5 minutes
4. 30 minutes
5. 2 hours

After five failed attempts, the delivery is marked failed. Manual retry is available in the admin UI.

## Backups

```bash
pocketpress backup create
```

Creates a `.tar.gz` snapshot containing:

- The SQLite database
- All local uploaded files
- A manifest with PocketPress version and timestamp

**Important:** Stop the server before restoring a backup:

```bash
pocketpress backup restore ~/pocketpress_backup_2026-08-02.tar.gz
```

S3-hosted files are not copied into backups, but the manifest records that they are external.

## Development

**Requirements:**

- Go 1.26 or later
- Node.js 22 or later (for the admin SPA build)

**Commands:**

```bash
make dev       # Run backend and admin dev servers together
make test      # Run Go and admin tests
make lint      # Check gofmt, go vet, and admin lint
make build     # Build the admin SPA and compile the binary
make clean     # Remove build artifacts
```

**End-to-end tests:**

```bash
cd e2e
npm ci
npx playwright install chromium
npm test
```

## License

MIT. See LICENSE for details.

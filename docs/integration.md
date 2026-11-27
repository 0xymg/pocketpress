# Connecting your frontend to PocketPress

PocketPress exposes all published content via a stable `/api/v1` REST API. This guide shows how to fetch and display PocketPress posts from your frontend.

## Official SDK (recommended)

Install `@pocketpress/sdk` (see `sdk/README.md`) for a typed client over the same public API:

```typescript
import { PocketPress, PocketPressError, verifyWebhookSignature } from "@pocketpress/sdk";

const pp = new PocketPress({
  baseUrl: process.env.POCKETPRESS_URL ?? "http://localhost:8090",
});

const { data: posts } = await pp.posts.list({ page: 1, featured: true });
const { data: found } = await pp.posts.list({ q: "release notes" });
const { data: post } = await pp.posts.get("hello-world");
const cover = pp.resolveUrl(post?.cover_image?.url);

// Draft preview (authorized token flow; not gated by public_api_enabled)
const draft = await pp.posts.preview("draft-slug", previewToken);

// Feed / sitemap URLs or raw XML
const rssUrl = pp.feeds.rssUrl();
```

Pass your framework's `fetch` when required (SvelteKit, some Next.js edge setups):

```typescript
const pp = new PocketPress({ baseUrl: CMS_URL, fetch });
```

The raw `fetch` examples below remain valid if you prefer not to add a dependency.

## Fetching posts

### Next.js (App Router)

```typescript
// app/blog/page.tsx
import { notFound } from "next/navigation";

const CMS_URL = process.env.NEXT_PUBLIC_CMS_URL || "http://localhost:8090";

async function getPosts() {
  const res = await fetch(`${CMS_URL}/api/v1/posts`, {
    next: { revalidate: 60 }, // Revalidate every 60 seconds
  });
  if (!res.ok) throw new Error("Failed to fetch posts");
  return res.json();
}

async function getPost(slug: string) {
  const res = await fetch(`${CMS_URL}/api/v1/posts/${slug}`, {
    next: { revalidate: 60 },
  });
  if (!res.ok) throw new Error("Post not found");
  return res.json();
}

export default async function Blog() {
  const { data: posts } = await getPosts();
  return (
    <ul>
      {posts.map((post: any) => (
        <li key={post.id}>
          <a href={`/blog/${post.slug}`}>{post.title}</a>
        </li>
      ))}
    </ul>
  );
}
```

### Astro

```astro
---
// src/pages/blog/index.astro
const CMS_URL = import.meta.env.PUBLIC_CMS_URL || "http://localhost:8090";
const res = await fetch(`${CMS_URL}/api/v1/posts`);
const { data: posts } = await res.json();
---

<ul>
  {posts.map((post: any) => (
    <li>
      <a href={`/blog/${post.slug}`}>{post.title}</a>
    </li>
  ))}
</ul>
```

### SvelteKit

```typescript
// src/routes/blog/+page.server.ts
import type { PageServerLoad } from "./$types";

export const load: PageServerLoad = async ({ fetch }) => {
  const res = await fetch(`http://localhost:8090/api/v1/posts`);
  const { data: posts } = await res.json();
  return { posts };
};
```

## Syndication feeds (RSS / Atom)

Published posts are also available as feeds (same visibility rule as the JSON API; disabled when `public_api_enabled` is false):

```text
GET /api/v1/feed.rss
GET /api/v1/feed.atom
```

Each feed returns up to 20 newest posts. Item/entry links use a post's SEO `canonical_url` when set; otherwise `{site_url}/{slug}`.

## Sitemap

A Sitemap 0.9 document lists the site homepage and every publicly-visible post:

```text
GET /api/v1/sitemap.xml
```

Post `<loc>` values use `canonical_url` when set; otherwise `{site_url}/{slug}`. Also gated by `public_api_enabled`.

## CORS setup

Cross-origin requests require explicit allowlist configuration. In the PocketPress admin, go to **Settings → General** and add your frontend URL to **CORS Origins**, or set `PP_CORS_ORIGINS` at startup:

```bash
docker run -e PP_CORS_ORIGINS=https://example.com,https://www.example.com \
  -p 8090:8090 -v pocketpress_data:/app/data pocketpress/pocketpress
```

## Previewing drafts

Generate a preview token from the admin UI, or use the admin API:

```bash
curl -X POST http://localhost:8090/api/admin/v1/posts/{post_id}/preview-token \
  -H "Authorization: Bearer <session_token>" \
  -H "Content-Type: application/json"
```

Response:

```json
{
  "token": "...",
  "expires_at": "2026-08-02T12:15:00Z",
  "preview_url": "https://cms.example.com/api/v1/preview/posts/draft-post?token=..."
}
```

Configure your frontend preview URL in **Settings → General**. Use a template like:

```
https://yoursite.com/preview?slug={slug}&token={token}
```

On your preview route, fetch the draft:

```typescript
// getServerSideProps or server-side (never in getStaticProps)
const res = await fetch(
  `https://cms.example.com/api/v1/preview/posts/${slug}?token=${token}`
);
if (!res.ok) return { notFound: true };
const { data: post } = await res.json();
```

Responses include `Cache-Control: no-store` and are never cached.

## Webhook-triggered rebuilds

When a post publishes, PocketPress sends a webhook. Verify the signature in your deploy handler:

```typescript
// Node.js — preferred via the SDK
import { verifyWebhookSignature } from "@pocketpress/sdk";

export async function handleWebhook(req: {
  headers: Record<string, string | undefined>;
  rawBody: string;
}) {
  const ok = verifyWebhookSignature({
    signature: req.headers["x-pocketpress-signature"],
    timestamp: req.headers["x-pocketpress-timestamp"],
    body: req.rawBody, // exact raw bytes PocketPress signed
    secret: process.env.POCKETPRESS_WEBHOOK_SECRET!,
  });
  if (!ok) {
    throw new Error("Invalid signature");
  }
  // Signature verified; trigger your rebuild
}
```

Configure the webhook URL and secret in the admin UI (**Settings → Webhooks**). PocketPress retries failed deliveries automatically.

## Response shape

Every post includes these fields. See `api/openapi.yaml` for full details:

```json
{
  "id": "post_id",
  "title": "Post title",
  "slug": "post-slug",
  "excerpt": "Short summary",
  "content_html": "<p>Sanitized HTML</p>",
  "cover_image": {
    "url": "/api/files/{id}/image.jpg",
    "width": null,
    "height": null,
    "alt": "Alt text"
  },
  "author": {
    "id": "user_id",
    "name": "Author name"
  },
  "categories": [
    {
      "id": "cat_id",
      "name": "Category",
      "slug": "category"
    }
  ],
  "tags": [
    {
      "id": "tag_id",
      "name": "Tag",
      "slug": "tag"
    }
  ],
  "featured": false,
  "seo": {
    "title": "Custom SEO title",
    "description": "Meta description",
    "canonical_url": "https://example.com/path",
    "og_image_url": "/api/files/{id}/og.jpg"
  },
  "published_at": "2026-08-02T12:00:00Z",
  "updated_at": "2026-08-02T12:00:00Z"
}
```

File URLs (`cover_image.url`, `seo.og_image_url`) are relative paths. Resolve them against the PocketPress instance URL (e.g., `PP_PUBLIC_URL`).

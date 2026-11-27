# `@pocketpress/sdk`

TypeScript/JavaScript client for the PocketPress public content API (`/api/v1`).

Zero runtime dependencies. Works in Node.js 18+, Bun, Deno, and modern browsers (webhook verification uses Node's `crypto` and is Node/Bun-oriented).

## Install

```bash
npm install @pocketpress/sdk
```

Until the package is published to npm, link a local checkout:

```bash
# from your app
npm install /path/to/pocketpress/sdk
```

Or, inside this repository while developing the SDK:

```bash
cd sdk && npm ci && npm run build
```
## Quick start

```ts
import { PocketPress, PocketPressError } from "@pocketpress/sdk";

const pp = new PocketPress({
  baseUrl: process.env.POCKETPRESS_URL ?? "http://localhost:8090",
});

const { data: posts, meta } = await pp.posts.list({ page: 1, per_page: 20 });

const { data: post, etag, notModified } = await pp.posts.get("hello-world");
if (!notModified && post) {
  const cover = pp.resolveUrl(post.cover_image?.url);
  console.log(post.title, cover, etag);
}

try {
  await pp.posts.get("missing");
} catch (err) {
  if (err instanceof PocketPressError && err.code === "post_not_found") {
    // handle 404
  }
}
```

Pass a framework-bound `fetch` when your runtime requires it:

```ts
// SvelteKit +page.server.ts
export const load = async ({ fetch }) => {
  const pp = new PocketPress({ baseUrl: "http://localhost:8090", fetch });
  return { posts: (await pp.posts.list()).data };
};
```

## API surface

| Method | Endpoint |
|---|---|
| `pp.health()` | `GET /healthz` |
| `pp.posts.list(params?)` | `GET /api/v1/posts` (supports `q` full-text search) |
| `pp.posts.get(slug, { etag? })` | `GET /api/v1/posts/{slug}` |
| `pp.posts.preview(slug, token)` | `GET /api/v1/preview/posts/{slug}` |
| `pp.categories.list()` / `.get(slug)` / `.listPosts(slug, params?)` | categories |
| `pp.tags.list()` / `.get(slug)` / `.listPosts(slug, params?)` | tags |
| `pp.feeds.rss()` / `.atom()` / `.sitemap()` | XML documents |
| `pp.feeds.rssUrl()` / `.atomUrl()` / `.sitemapUrl()` | absolute feed URLs |
| `pp.resolveUrl(path)` | resolve `/api/files/...` against `baseUrl` |

Types (`Post`, `Category`, `Tag`, …) match `api/openapi.yaml`.

## Webhook verification (Node.js)

```ts
import { verifyWebhookSignature } from "@pocketpress/sdk";

const ok = verifyWebhookSignature({
  signature: req.headers["x-pocketpress-signature"],
  timestamp: req.headers["x-pocketpress-timestamp"],
  body: rawBodyString,
  secret: process.env.POCKETPRESS_WEBHOOK_SECRET!,
});
```

## Development

```bash
cd sdk
npm ci
npm test
npm run lint
npm run build
```

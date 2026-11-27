import { describe, expect, it, vi } from "vitest";
import { PocketPress, PocketPressError } from "../src/index.js";
import type { Post } from "../src/types.js";

const samplePost: Post = {
  id: "p1",
  title: "Hello",
  slug: "hello",
  excerpt: "ex",
  content_html: "<p>Hi</p>",
  cover_image: {
    url: "/api/files/m1/cover.jpg",
    width: null,
    height: null,
    alt: "Cover",
  },
  author: { id: "u1", name: "Ada" },
  categories: [{ id: "c1", name: "News", slug: "news" }],
  tags: [{ id: "t1", name: "Go", slug: "go" }],
  featured: true,
  seo: {
    title: null,
    description: null,
    canonical_url: "https://example.com/hello",
    og_image_url: null,
  },
  published_at: "2026-08-02T12:00:00Z",
  updated_at: "2026-08-02T12:00:00Z",
};

function jsonResponse(body: unknown, init: ResponseInit = {}): Response {
  const headers = new Headers(init.headers);
  if (!headers.has("content-type")) {
    headers.set("content-type", "application/json");
  }
  return new Response(JSON.stringify(body), { ...init, headers });
}

describe("PocketPress client", () => {
  it("requires baseUrl", () => {
    expect(() => new PocketPress({ baseUrl: "" })).toThrow(TypeError);
  });

  it("strips trailing slashes from baseUrl", () => {
    const client = new PocketPress({ baseUrl: "https://cms.example.com/" });
    expect(client.baseUrl).toBe("https://cms.example.com");
  });

  it("lists posts with query params", async () => {
    const fetchMock = vi.fn(async (input: string | URL) => {
      expect(String(input)).toBe(
        "https://cms.example.com/api/v1/posts?page=2&per_page=10&featured=true&sort=-published_at",
      );
      return jsonResponse({
        data: [samplePost],
        meta: { page: 2, per_page: 10, total_items: 1, total_pages: 1 },
      });
    });

    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });
    const result = await client.posts.list({
      page: 2,
      per_page: 10,
      featured: true,
      sort: "-published_at",
    });
    expect(result.data).toHaveLength(1);
    expect(result.meta.page).toBe(2);
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it("gets a post and returns etag", async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse(
        { data: samplePost },
        { headers: { ETag: '"abc123"' } },
      ),
    );
    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });
    const result = await client.posts.get("hello");
    expect(result.notModified).toBe(false);
    expect(result.data?.slug).toBe("hello");
    expect(result.etag).toBe('"abc123"');
  });

  it("handles 304 Not Modified with If-None-Match", async () => {
    const fetchMock = vi.fn(async (_input, init) => {
      const headers = new Headers(init?.headers);
      expect(headers.get("If-None-Match")).toBe('"abc123"');
      return new Response(null, {
        status: 304,
        headers: { ETag: '"abc123"' },
      });
    });
    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });
    const result = await client.posts.get("hello", { etag: '"abc123"' });
    expect(result.notModified).toBe(true);
    expect(result.data).toBeUndefined();
    expect(result.etag).toBe('"abc123"');
  });

  it("throws PocketPressError with API code on 404", async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse(
        {
          error: {
            code: "post_not_found",
            message: "The requested post was not found.",
            details: null,
          },
        },
        { status: 404 },
      ),
    );
    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });
    await expect(client.posts.get("missing")).rejects.toMatchObject({
      name: "PocketPressError",
      status: 404,
      code: "post_not_found",
    });
    await expect(client.posts.get("missing")).rejects.toBeInstanceOf(
      PocketPressError,
    );
  });

  it("previews a post with token", async () => {
    const fetchMock = vi.fn(async (input: string | URL) => {
      expect(String(input)).toBe(
        "https://cms.example.com/api/v1/preview/posts/draft?token=tok",
      );
      return jsonResponse({ data: { ...samplePost, slug: "draft" } });
    });
    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });
    const post = await client.posts.preview("draft", "tok");
    expect(post.slug).toBe("draft");
  });

  it("lists and gets categories and tags", async () => {
    const fetchMock = vi.fn(async (input: string | URL) => {
      const url = String(input);
      if (url.endsWith("/api/v1/categories")) {
        return jsonResponse({
          data: [{ id: "c1", name: "News", slug: "news", description: "" }],
        });
      }
      if (url.endsWith("/api/v1/categories/news")) {
        return jsonResponse({
          data: { id: "c1", name: "News", slug: "news", description: "d" },
        });
      }
      if (url.includes("/api/v1/categories/news/posts")) {
        return jsonResponse({
          data: [samplePost],
          meta: { page: 1, per_page: 20, total_items: 1, total_pages: 1 },
        });
      }
      if (url.endsWith("/api/v1/tags")) {
        return jsonResponse({
          data: [{ id: "t1", name: "Go", slug: "go" }],
        });
      }
      if (url.endsWith("/api/v1/tags/go")) {
        return jsonResponse({ data: { id: "t1", name: "Go", slug: "go" } });
      }
      if (url.includes("/api/v1/tags/go/posts")) {
        return jsonResponse({
          data: [samplePost],
          meta: { page: 1, per_page: 20, total_items: 1, total_pages: 1 },
        });
      }
      return jsonResponse({ error: { code: "x", message: "bad", details: null } }, { status: 500 });
    });

    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });

    expect(await client.categories.list()).toEqual([
      { id: "c1", name: "News", slug: "news", description: "" },
    ]);
    expect((await client.categories.get("news")).slug).toBe("news");
    expect((await client.categories.listPosts("news")).data).toHaveLength(1);
    expect(await client.tags.list()).toEqual([
      { id: "t1", name: "Go", slug: "go" },
    ]);
    expect((await client.tags.get("go")).slug).toBe("go");
    expect((await client.tags.listPosts("go")).data).toHaveLength(1);
  });

  it("fetches health and resolves relative file URLs", async () => {
    const fetchMock = vi.fn(async () =>
      jsonResponse({ status: "ok", version: "0.2.0" }),
    );
    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });
    expect(await client.health()).toEqual({ status: "ok", version: "0.2.0" });
    expect(client.resolveUrl("/api/files/m1/a.jpg")).toBe(
      "https://cms.example.com/api/files/m1/a.jpg",
    );
    expect(client.resolveUrl("https://cdn.example.com/x.jpg")).toBe(
      "https://cdn.example.com/x.jpg",
    );
    expect(client.resolveUrl(null)).toBeNull();
  });

  it("exposes feed URLs and fetches XML bodies", async () => {
    const fetchMock = vi.fn(async (input: string | URL) => {
      const url = String(input);
      if (url.endsWith("/feed.rss")) {
        return new Response("<rss/>", {
          headers: { "content-type": "application/rss+xml" },
        });
      }
      if (url.endsWith("/feed.atom")) {
        return new Response("<feed/>", {
          headers: { "content-type": "application/atom+xml" },
        });
      }
      if (url.endsWith("/sitemap.xml")) {
        return new Response("<urlset/>", {
          headers: { "content-type": "application/xml" },
        });
      }
      return new Response("nope", { status: 404 });
    });
    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });
    expect(client.feeds.rssUrl()).toBe(
      "https://cms.example.com/api/v1/feed.rss",
    );
    expect(client.feeds.atomUrl()).toBe(
      "https://cms.example.com/api/v1/feed.atom",
    );
    expect(client.feeds.sitemapUrl()).toBe(
      "https://cms.example.com/api/v1/sitemap.xml",
    );
    expect(await client.feeds.rss()).toBe("<rss/>");
    expect(await client.feeds.atom()).toBe("<feed/>");
    expect(await client.feeds.sitemap()).toBe("<urlset/>");
  });

  it("encodes slug path segments", async () => {
    const fetchMock = vi.fn(async (input: string | URL) => {
      expect(String(input)).toBe(
        "https://cms.example.com/api/v1/posts/hello%2Fworld",
      );
      return jsonResponse({ data: samplePost });
    });
    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: fetchMock,
    });
    await client.posts.get("hello/world");
  });

  it("wraps network failures", async () => {
    const client = new PocketPress({
      baseUrl: "https://cms.example.com",
      fetch: async () => {
        throw new TypeError("offline");
      },
    });
    await expect(client.posts.list()).rejects.toMatchObject({
      code: "network_error",
      status: 0,
    });
  });
});

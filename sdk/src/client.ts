import { PocketPressError } from "./errors.js";
import type {
  Category,
  DataResponse,
  GetPostResult,
  HealthResponse,
  Post,
  PostListParams,
  PostListResponse,
  Tag,
} from "./types.js";

export type FetchLike = (
  input: string | URL,
  init?: RequestInit,
) => Promise<Response>;

export interface PocketPressOptions {
  /**
   * Base URL of the PocketPress instance (no trailing slash),
   * e.g. `https://cms.example.com` or `http://localhost:8090`.
   */
  baseUrl: string;
  /**
   * Custom fetch implementation. Defaults to global `fetch`.
   * Pass a framework-bound fetch (Next.js, SvelteKit) when needed.
   */
  fetch?: FetchLike;
  /** Extra headers sent on every request. */
  headers?: HeadersInit;
}

function trimTrailingSlash(url: string): string {
  return url.replace(/\/+$/, "");
}

function encodePathSegment(value: string): string {
  return encodeURIComponent(value);
}

function buildQuery(params: Record<string, string | number | boolean | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined) continue;
    search.set(key, String(value));
  }
  const qs = search.toString();
  return qs ? `?${qs}` : "";
}

/**
 * Typed client for the PocketPress public `/api/v1` content API.
 *
 * Zero runtime dependencies — uses the platform `fetch` (or an injected one).
 */
export class PocketPress {
  readonly baseUrl: string;
  private readonly fetchImpl: FetchLike;
  private readonly defaultHeaders: HeadersInit;

  readonly posts: PostsResource;
  readonly categories: CategoriesResource;
  readonly tags: TagsResource;
  readonly feeds: FeedsResource;

  constructor(options: PocketPressOptions) {
    if (!options.baseUrl || typeof options.baseUrl !== "string") {
      throw new TypeError("PocketPress requires a non-empty baseUrl string");
    }
    this.baseUrl = trimTrailingSlash(options.baseUrl);
    this.fetchImpl = options.fetch ?? globalThis.fetch.bind(globalThis);
    this.defaultHeaders = options.headers ?? {};

    this.posts = new PostsResource(this);
    this.categories = new CategoriesResource(this);
    this.tags = new TagsResource(this);
    this.feeds = new FeedsResource(this);
  }

  /** Liveness probe (`GET /healthz`). Not gated by `public_api_enabled`. */
  async health(): Promise<HealthResponse> {
    return this.requestJson<HealthResponse>("/healthz");
  }

  /**
   * Resolve a relative PocketPress file path (e.g. `/api/files/...`) against
   * this client's `baseUrl`. Absolute URLs are returned unchanged.
   */
  resolveUrl(pathOrUrl: string | null | undefined): string | null {
    if (pathOrUrl == null || pathOrUrl === "") {
      return null;
    }
    if (/^https?:\/\//i.test(pathOrUrl)) {
      return pathOrUrl;
    }
    const path = pathOrUrl.startsWith("/") ? pathOrUrl : `/${pathOrUrl}`;
    return `${this.baseUrl}${path}`;
  }

  /** Absolute URL for a public API path (e.g. `/api/v1/posts`). */
  url(path: string): string {
    const normalized = path.startsWith("/") ? path : `/${path}`;
    return `${this.baseUrl}${normalized}`;
  }

  /** @internal */
  async requestJson<T>(
    path: string,
    init?: RequestInit & { expectStatus?: number[] },
  ): Promise<T> {
    const res = await this.rawRequest(path, init);
    const expect = init?.expectStatus ?? [200];
    if (!expect.includes(res.status)) {
      throw await this.errorFromResponse(res);
    }
    if (res.status === 204 || res.status === 304) {
      return undefined as T;
    }
    return (await res.json()) as T;
  }

  /** @internal */
  async requestText(
    path: string,
    init?: RequestInit,
  ): Promise<{ body: string; contentType: string | null }> {
    const res = await this.rawRequest(path, init);
    if (res.status !== 200) {
      throw await this.errorFromResponse(res);
    }
    return {
      body: await res.text(),
      contentType: res.headers.get("content-type"),
    };
  }

  /** @internal */
  async rawRequest(path: string, init?: RequestInit): Promise<Response> {
    const headers = new Headers(this.defaultHeaders);
    if (init?.headers) {
      const extra = new Headers(init.headers);
      extra.forEach((value, key) => {
        headers.set(key, value);
      });
    }
    if (!headers.has("Accept")) {
      headers.set("Accept", "application/json");
    }

    let res: Response;
    try {
      res = await this.fetchImpl(this.url(path), {
        ...init,
        headers,
      });
    } catch (cause) {
      throw new PocketPressError("Network request failed", {
        status: 0,
        code: "network_error",
        cause,
      });
    }
    return res;
  }

  /** @internal */
  async errorFromResponse(res: Response): Promise<PocketPressError> {
    let body: { error?: { code: string; message: string; details: unknown } } | null =
      null;
    const contentType = res.headers.get("content-type") ?? "";
    if (contentType.includes("application/json")) {
      try {
        body = (await res.json()) as typeof body;
      } catch {
        body = null;
      }
    }
    return PocketPressError.fromEnvelope(
      res.status,
      body,
      `PocketPress request failed with status ${res.status}`,
    );
  }
}

class PostsResource {
  constructor(private readonly client: PocketPress) {}

  /** `GET /api/v1/posts` — list published posts. */
  list(params: PostListParams = {}): Promise<PostListResponse> {
    const qs = buildQuery({
      page: params.page,
      per_page: params.per_page,
      category: params.category,
      tag: params.tag,
      featured: params.featured,
      author: params.author,
      sort: params.sort,
      q: params.q,
    });
    return this.client.requestJson<PostListResponse>(`/api/v1/posts${qs}`);
  }

  /**
   * `GET /api/v1/posts/{slug}` — get a published post.
   * Pass `etag` to send `If-None-Match` and receive `notModified: true` on 304.
   */
  async get(
    slug: string,
    options: { etag?: string } = {},
  ): Promise<GetPostResult> {
    const headers: Record<string, string> = {};
    if (options.etag) {
      headers["If-None-Match"] = options.etag;
    }
    const res = await this.client.rawRequest(
      `/api/v1/posts/${encodePathSegment(slug)}`,
      { headers },
    );

    const responseEtag = res.headers.get("etag") ?? undefined;

    if (res.status === 304) {
      const result: GetPostResult = { notModified: true };
      if (responseEtag !== undefined) {
        result.etag = responseEtag;
      }
      return result;
    }

    if (res.status !== 200) {
      throw await this.client.errorFromResponse(res);
    }

    const json = (await res.json()) as DataResponse<Post>;
    const result: GetPostResult = {
      data: json.data,
      notModified: false,
    };
    if (responseEtag !== undefined) {
      result.etag = responseEtag;
    }
    return result;
  }

  /**
   * `GET /api/v1/preview/posts/{slug}?token=...` — preview any status with a
   * signed token. Not gated by `public_api_enabled`.
   */
  async preview(slug: string, token: string): Promise<Post> {
    const qs = buildQuery({ token });
    const json = await this.client.requestJson<DataResponse<Post>>(
      `/api/v1/preview/posts/${encodePathSegment(slug)}${qs}`,
    );
    return json.data;
  }
}

class CategoriesResource {
  constructor(private readonly client: PocketPress) {}

  /** `GET /api/v1/categories` */
  async list(): Promise<Category[]> {
    const json = await this.client.requestJson<DataResponse<Category[]>>(
      "/api/v1/categories",
    );
    return json.data;
  }

  /** `GET /api/v1/categories/{slug}` */
  async get(slug: string): Promise<Category> {
    const json = await this.client.requestJson<DataResponse<Category>>(
      `/api/v1/categories/${encodePathSegment(slug)}`,
    );
    return json.data;
  }

  /** `GET /api/v1/categories/{slug}/posts` */
  listPosts(
    slug: string,
    params: Omit<PostListParams, "category"> = {},
  ): Promise<PostListResponse> {
    const qs = buildQuery({
      page: params.page,
      per_page: params.per_page,
      tag: params.tag,
      featured: params.featured,
      author: params.author,
      sort: params.sort,
      q: params.q,
    });
    return this.client.requestJson<PostListResponse>(
      `/api/v1/categories/${encodePathSegment(slug)}/posts${qs}`,
    );
  }
}

class TagsResource {
  constructor(private readonly client: PocketPress) {}

  /** `GET /api/v1/tags` */
  async list(): Promise<Tag[]> {
    const json = await this.client.requestJson<DataResponse<Tag[]>>(
      "/api/v1/tags",
    );
    return json.data;
  }

  /** `GET /api/v1/tags/{slug}` */
  async get(slug: string): Promise<Tag> {
    const json = await this.client.requestJson<DataResponse<Tag>>(
      `/api/v1/tags/${encodePathSegment(slug)}`,
    );
    return json.data;
  }

  /** `GET /api/v1/tags/{slug}/posts` */
  listPosts(
    slug: string,
    params: Omit<PostListParams, "tag"> = {},
  ): Promise<PostListResponse> {
    const qs = buildQuery({
      page: params.page,
      per_page: params.per_page,
      category: params.category,
      featured: params.featured,
      author: params.author,
      sort: params.sort,
      q: params.q,
    });
    return this.client.requestJson<PostListResponse>(
      `/api/v1/tags/${encodePathSegment(slug)}/posts${qs}`,
    );
  }
}

class FeedsResource {
  constructor(private readonly client: PocketPress) {}

  /** Absolute URL of the RSS 2.0 feed. */
  rssUrl(): string {
    return this.client.url("/api/v1/feed.rss");
  }

  /** Absolute URL of the Atom 1.0 feed. */
  atomUrl(): string {
    return this.client.url("/api/v1/feed.atom");
  }

  /** Absolute URL of the Sitemap 0.9 document. */
  sitemapUrl(): string {
    return this.client.url("/api/v1/sitemap.xml");
  }

  /** Fetch the RSS 2.0 XML document as text. */
  async rss(): Promise<string> {
    const { body } = await this.client.requestText("/api/v1/feed.rss", {
      headers: { Accept: "application/rss+xml, application/xml, text/xml" },
    });
    return body;
  }

  /** Fetch the Atom 1.0 XML document as text. */
  async atom(): Promise<string> {
    const { body } = await this.client.requestText("/api/v1/feed.atom", {
      headers: { Accept: "application/atom+xml, application/xml, text/xml" },
    });
    return body;
  }

  /** Fetch the Sitemap 0.9 XML document as text. */
  async sitemap(): Promise<string> {
    const { body } = await this.client.requestText("/api/v1/sitemap.xml", {
      headers: { Accept: "application/xml, text/xml" },
    });
    return body;
  }
}

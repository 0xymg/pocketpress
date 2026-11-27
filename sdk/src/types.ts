/** Pagination metadata returned with post list responses. */
export interface Meta {
  page: number;
  per_page: number;
  total_items: number;
  total_pages: number;
}

/** Public author summary embedded in a post. */
export interface Author {
  id: string;
  name: string;
}

/** Category or tag reference embedded in a post. */
export interface Term {
  id: string;
  name: string;
  slug: string;
}

/** Cover image metadata. File `url` is relative to the PocketPress origin. */
export interface CoverImage {
  url: string;
  width: number | null;
  height: number | null;
  alt: string;
}

/** On-demand image transform options for `/api/files/...` URLs. */
export interface ImageTransformOptions {
  /** Target width in pixels (1–4096). */
  w?: number;
  /** Target height in pixels (1–4096). */
  h?: number;
  /** Resize mode. `cover`/`fill` require both `w` and `h`. */
  fit?: "contain" | "cover" | "fill";
  /** Output format. `jpg` is accepted by the server as an alias for `jpeg`. */
  fm?: "jpeg" | "jpg" | "png";
  /** JPEG quality 1–100 (default 82 on the server). */
  q?: number;
}

/** SEO metadata on a post. */
export interface Seo {
  title: string | null;
  description: string | null;
  canonical_url: string | null;
  /** Relative file URL of the Open Graph image, or null. */
  og_image_url: string | null;
}

/**
 * Published post shape from the public API.
 * Never includes admin-only fields (status, content_json, scheduled_for, etc.).
 */
export interface Post {
  id: string;
  title: string;
  slug: string;
  excerpt: string;
  content_html: string;
  cover_image: CoverImage | null;
  author: Author;
  categories: Term[];
  tags: Term[];
  featured: boolean;
  seo: Seo;
  published_at: string;
  updated_at: string;
}

export interface Category {
  id: string;
  name: string;
  slug: string;
  description: string;
}

export interface Tag {
  id: string;
  name: string;
  slug: string;
}

export type PostSort =
  | "published_at"
  | "-published_at"
  | "title"
  | "-title";

/** Query filters shared by post list endpoints. */
export interface PostListParams {
  page?: number;
  per_page?: number;
  category?: string;
  tag?: string;
  featured?: boolean;
  author?: string;
  sort?: PostSort;
  /** Full-text search (`q`) over title, excerpt, and body. */
  q?: string;
}

export interface PostListResponse {
  data: Post[];
  meta: Meta;
}

export interface DataResponse<T> {
  data: T;
}

export interface HealthResponse {
  status: "ok";
  version: string;
}

/** Result of GET /api/v1/posts/{slug} with optional ETag conditional request. */
export interface GetPostResult {
  /** Present when the post changed (or no If-None-Match was sent). */
  data?: Post;
  /** True when the server returned 304 Not Modified. */
  notModified: boolean;
  /** ETag from the response, when present. */
  etag?: string;
}

export interface PocketPressErrorBody {
  code: string;
  message: string;
  details: unknown;
}

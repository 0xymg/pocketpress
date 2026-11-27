export interface HealthStatus {
	status: string;
	version: string;
}

/**
 * Calls the PocketPress API health endpoint.
 * In dev this is proxied by Vite to the local backend (see vite.config.ts).
 * In production the API and admin SPA are served from the same origin.
 */
export async function fetchHealth(): Promise<HealthStatus> {
	const response = await fetch('/healthz');

	if (!response.ok) {
		throw new Error(`Health check failed with status ${response.status}`);
	}

	return (await response.json()) as HealthStatus;
}

// ---------------------------------------------------------------------------
// Admin API v1 — see the frozen contract in the Phase 2 task brief and
// PocketPress_Development_Plan.md section 9. All requests are cookie-based
// (server-side sessions), hence `credentials: 'same-origin'` on every call.
// ---------------------------------------------------------------------------

const ADMIN_API_BASE = '/api/admin/v1';

export type UserRole = 'owner' | 'editor';

export interface User {
	id: string;
	name: string;
	email: string;
	role: UserRole;
	slug: string;
	bio: string;
	website: string;
	location: string;
	avatar: string;
	active: boolean;
	last_login_at: string | null;
	created_at: string;
}

export interface SetupStatus {
	initialized: boolean;
}

export interface SetupPayload {
	site_name: string;
	site_url: string;
	owner_name: string;
	owner_email: string;
	password: string;
	timezone: string;
}

export interface LoginPayload {
	email: string;
	password: string;
}

export interface CreateUserPayload {
	name: string;
	email: string;
	password: string;
	role: UserRole;
}

export interface UpdateUserPayload {
	name?: string;
	slug?: string;
	bio?: string;
	website?: string;
	location?: string;
	avatar?: string;
	role?: UserRole;
	active?: boolean;
	password?: string;
}

export interface UpdateProfilePayload {
	name?: string;
	slug?: string;
	bio?: string;
	website?: string;
	location?: string;
	avatar?: string;
	password?: string;
}

interface ApiErrorBody {
	error?: {
		code?: string;
		message?: string;
		details?: Record<string, string>;
	};
}

/** Error thrown for any non-2xx admin API response, carrying the server's error code/details. */
export class ApiError extends Error {
	readonly status: number;
	readonly code: string;
	readonly details?: Record<string, string>;

	constructor(status: number, code: string, message: string, details?: Record<string, string>) {
		super(message);
		this.name = 'ApiError';
		this.status = status;
		this.code = code;
		this.details = details;
	}
}

async function adminFetch<T>(path: string, init?: RequestInit): Promise<T> {
	// Multipart uploads (media) pass a FormData body and must NOT get a manual
	// Content-Type — the browser sets `multipart/form-data; boundary=...`
	// itself. Every other call sends a JSON string body.
	const isFormData = typeof FormData !== 'undefined' && init?.body instanceof FormData;
	const response = await fetch(`${ADMIN_API_BASE}${path}`, {
		...init,
		credentials: 'same-origin',
		headers: {
			...(init?.body && !isFormData ? { 'Content-Type': 'application/json' } : {}),
			...(init?.headers ?? {})
		}
	});

	// 204 No Content (e.g. logout) — nothing to parse.
	if (response.status === 204) {
		return undefined as T;
	}

	const text = await response.text();
	let body: unknown = null;
	if (text) {
		try {
			body = JSON.parse(text);
		} catch {
			body = null;
		}
	}

	if (!response.ok) {
		const error = (body as ApiErrorBody | null)?.error;
		throw new ApiError(
			response.status,
			error?.code ?? 'unknown_error',
			error?.message ?? `Request failed with status ${response.status}`,
			error?.details
		);
	}

	return body as T;
}

export function getSetupStatus(): Promise<SetupStatus> {
	return adminFetch<SetupStatus>('/setup/status');
}

export function submitSetup(payload: SetupPayload): Promise<{ user: User }> {
	return adminFetch<{ user: User }>('/setup', {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function login(payload: LoginPayload): Promise<{ user: User }> {
	return adminFetch<{ user: User }>('/auth/login', {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function logout(): Promise<void> {
	return adminFetch<void>('/auth/logout', { method: 'POST' });
}

export function getMe(): Promise<{ user: User }> {
	return adminFetch<{ user: User }>('/auth/me');
}

export function listUsers(): Promise<{ users: User[] }> {
	return adminFetch<{ users: User[] }>('/users');
}

export function createUser(payload: CreateUserPayload): Promise<{ user: User }> {
	return adminFetch<{ user: User }>('/users', {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function updateUser(id: string, payload: UpdateUserPayload): Promise<{ user: User }> {
	return adminFetch<{ user: User }>(`/users/${id}`, {
		method: 'PATCH',
		body: JSON.stringify(payload)
	});
}

/** PATCH /auth/me — any signed-in user can edit their own author profile. */
export function updateProfile(payload: UpdateProfilePayload): Promise<{ user: User }> {
	return adminFetch<{ user: User }>('/auth/me', {
		method: 'PATCH',
		body: JSON.stringify(payload)
	});
}

// ---------------------------------------------------------------------------
// Categories, tags, authors and posts (Phase 3 — plan section 9).
// ---------------------------------------------------------------------------

export interface Category {
	id: string;
	name: string;
	slug: string;
	description: string | null;
	created_at: string;
	updated_at: string;
}

export interface Tag {
	id: string;
	name: string;
	slug: string;
	created_at: string;
	updated_at: string;
}

export interface CategoryPayload {
	name: string;
	slug?: string;
	description?: string;
}

export interface TagPayload {
	name: string;
	slug?: string;
}

export interface Author {
	id: string;
	name: string;
	slug: string;
}

/** Category/tag reference shape as it appears embedded on a post. */
export interface PostTaxonomyRef {
	id: string;
	name: string;
	slug: string;
}

export type PostStatus = 'draft' | 'scheduled' | 'published' | 'archived';

/** Media reference embedded on a post's cover_image/og_image (Phase 4 media contract). */
export interface PostMediaRef {
	id: string;
	url: string;
	alt: string;
	width: number;
	height: number;
}

export interface PostSummary {
	id: string;
	title: string;
	slug: string;
	status: PostStatus;
	excerpt: string;
	featured: boolean;
	author: Author;
	categories: PostTaxonomyRef[];
	tags: PostTaxonomyRef[];
	cover_image: PostMediaRef | null;
	og_image: PostMediaRef | null;
	scheduled_for: string | null;
	published_at: string | null;
	created_at: string;
	updated_at: string;
}

export interface PostFull extends PostSummary {
	/** Canonical editor document — serialized Lexical editor state JSON. */
	content_json: string;
	/** Server-sanitized HTML for immediate rendering. */
	content_html: string;
	seo_title: string;
	seo_description: string;
	canonical_url: string;
}

export interface PostListMeta {
	page: number;
	per_page: number;
	total_items: number;
	total_pages: number;
}

export interface PostListResult {
	posts: PostSummary[];
	meta: PostListMeta;
}

export interface PostListParams {
	page?: number;
	per_page?: number;
	status?: PostStatus;
	category?: string;
	search?: string;
	/** Admin sort allowlist: -updated_at (default) | updated_at | title | -title. */
	sort?: string;
}

export interface PostPayload {
	title: string;
	slug?: string;
	excerpt?: string;
	/** Serialized Lexical editor state JSON — canonical document. */
	content_json?: string;
	/** HTML exported from the editor; the server re-sanitizes it. */
	content_html?: string;
	// No `status` field: status changes only go through the publish/unpublish/
	// schedule/archive ops below (Phase 6 — plan section 9). A newly created
	// post defaults to "draft" server-side.
	author?: string;
	categories?: string[];
	tags?: string[];
	featured?: boolean;
	seo_title?: string;
	seo_description?: string;
	canonical_url?: string;
	/** Media id to set as the cover image; "" clears it; omitted leaves it unchanged. */
	cover_image?: string;
	/** Media id to set as the Open Graph image; "" clears it; omitted leaves it unchanged. */
	og_image?: string;
}

function buildQuery(params: Record<string, string | number | undefined>): string {
	const search = new URLSearchParams();
	for (const [key, value] of Object.entries(params)) {
		if (value === undefined || value === '') continue;
		search.set(key, String(value));
	}
	const qs = search.toString();
	return qs ? `?${qs}` : '';
}

export function listCategories(): Promise<{ categories: Category[] }> {
	return adminFetch<{ categories: Category[] }>('/categories');
}

export function createCategory(payload: CategoryPayload): Promise<{ category: Category }> {
	return adminFetch<{ category: Category }>('/categories', {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function updateCategory(
	id: string,
	payload: Partial<CategoryPayload>
): Promise<{ category: Category }> {
	return adminFetch<{ category: Category }>(`/categories/${id}`, {
		method: 'PATCH',
		body: JSON.stringify(payload)
	});
}

export function deleteCategory(id: string): Promise<void> {
	return adminFetch<void>(`/categories/${id}`, { method: 'DELETE' });
}

export function listTags(): Promise<{ tags: Tag[] }> {
	return adminFetch<{ tags: Tag[] }>('/tags');
}

export function createTag(payload: TagPayload): Promise<{ tag: Tag }> {
	return adminFetch<{ tag: Tag }>('/tags', {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function updateTag(id: string, payload: Partial<TagPayload>): Promise<{ tag: Tag }> {
	return adminFetch<{ tag: Tag }>(`/tags/${id}`, {
		method: 'PATCH',
		body: JSON.stringify(payload)
	});
}

export function deleteTag(id: string): Promise<void> {
	return adminFetch<void>(`/tags/${id}`, { method: 'DELETE' });
}

export function listAuthors(): Promise<{ authors: Author[] }> {
	return adminFetch<{ authors: Author[] }>('/authors');
}

export function listPosts(params: PostListParams = {}): Promise<PostListResult> {
	const qs = buildQuery({
		page: params.page,
		per_page: params.per_page,
		status: params.status,
		category: params.category,
		search: params.search,
		sort: params.sort ?? '-updated_at'
	});
	return adminFetch<PostListResult>(`/posts${qs}`);
}

export function createPost(payload: PostPayload): Promise<{ post: PostFull }> {
	return adminFetch<{ post: PostFull }>('/posts', {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function getPost(id: string): Promise<{ post: PostFull }> {
	return adminFetch<{ post: PostFull }>(`/posts/${id}`);
}

export function updatePost(id: string, payload: Partial<PostPayload>): Promise<{ post: PostFull }> {
	return adminFetch<{ post: PostFull }>(`/posts/${id}`, {
		method: 'PATCH',
		body: JSON.stringify(payload)
	});
}

export function deletePost(id: string): Promise<void> {
	return adminFetch<void>(`/posts/${id}`, { method: 'DELETE' });
}

export function duplicatePost(id: string): Promise<{ post: PostFull }> {
	return adminFetch<{ post: PostFull }>(`/posts/${id}/duplicate`, { method: 'POST' });
}

/** One successfully imported Markdown document. */
export interface MarkdownImportSuccess {
	filename: string;
	post: PostFull;
	warning?: string;
}

/** One Markdown document that failed validation or conversion. */
export interface MarkdownImportFailure {
	filename: string;
	code: string;
	message: string;
	details?: Record<string, string>;
}

export interface MarkdownImportResult {
	imported: MarkdownImportSuccess[];
	failed: MarkdownImportFailure[];
}

/**
 * Import one or more Markdown files (.md / .markdown) as posts.
 * Uses multipart field name "files" (same contract as the admin API).
 */
export function importMarkdownFiles(files: File[]): Promise<MarkdownImportResult> {
	const formData = new FormData();
	for (const file of files) {
		formData.append('files', file);
	}
	return adminFetch<MarkdownImportResult>('/posts/import/markdown', {
		method: 'POST',
		body: formData
	});
}

/** One successfully imported Ghost/WordPress post. */
export interface ExportImportSuccess {
	source_key: string;
	post: PostFull;
	warning?: string;
}

/** One Ghost/WordPress post that failed validation or conversion. */
export interface ExportImportFailure {
	source_key: string;
	code: string;
	message: string;
	details?: Record<string, string>;
}

export interface ExportImportResult {
	imported: ExportImportSuccess[];
	failed: ExportImportFailure[];
}

/**
 * Import posts from a Ghost content export JSON file.
 * Uses multipart field name "file".
 */
export function importGhostExport(file: File): Promise<ExportImportResult> {
	const formData = new FormData();
	formData.append('file', file);
	return adminFetch<ExportImportResult>('/posts/import/ghost', {
		method: 'POST',
		body: formData
	});
}

/**
 * Import posts from a WordPress WXR (.xml) export file.
 * Uses multipart field name "file".
 */
export function importWordPressExport(file: File): Promise<ExportImportResult> {
	const formData = new FormData();
	formData.append('file', file);
	return adminFetch<ExportImportResult>('/posts/import/wordpress', {
		method: 'POST',
		body: formData
	});
}

// ---------------------------------------------------------------------------
// Publishing, scheduling, archiving and preview (Phase 6 — frozen contract,
// plan sections 7 and 9). Every op returns the full updated post so the
// caller can re-baseline the editor form from it in one shot.
// ---------------------------------------------------------------------------

export function publishPost(id: string): Promise<{ post: PostFull }> {
	return adminFetch<{ post: PostFull }>(`/posts/${id}/publish`, { method: 'POST' });
}

export function unpublishPost(id: string): Promise<{ post: PostFull }> {
	return adminFetch<{ post: PostFull }>(`/posts/${id}/unpublish`, { method: 'POST' });
}

export interface SchedulePostPayload {
	/** RFC3339 timestamp, must be in the future. */
	scheduled_for: string;
}

export function schedulePost(
	id: string,
	payload: SchedulePostPayload
): Promise<{ post: PostFull }> {
	return adminFetch<{ post: PostFull }>(`/posts/${id}/schedule`, {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function archivePost(id: string): Promise<{ post: PostFull }> {
	return adminFetch<{ post: PostFull }>(`/posts/${id}/archive`, { method: 'POST' });
}

export interface PreviewTokenResult {
	token: string;
	expires_at: string;
	/** null when no preview_url_template is configured in settings. */
	preview_url: string | null;
}

export function createPreviewToken(id: string): Promise<PreviewTokenResult> {
	return adminFetch<PreviewTokenResult>(`/posts/${id}/preview-token`, { method: 'POST' });
}

// ---------------------------------------------------------------------------
// Settings (Phase 6, OWNER only — plan section 9 /settings/general).
// ---------------------------------------------------------------------------

export interface Settings {
	site_name: string;
	site_url: string;
	admin_title: string;
	locale: string;
	timezone: string;
	default_author: string | null;
	cors_origins: string[];
	public_api_enabled: boolean;
	preview_url_template: string | null;
	webhook_user_agent: string;
}

/** PATCH is partial — send only the fields being changed. */
export type SettingsPayload = Partial<Settings>;

export function getSettings(): Promise<{ settings: Settings }> {
	return adminFetch<{ settings: Settings }>('/settings');
}

export function updateSettings(payload: SettingsPayload): Promise<{ settings: Settings }> {
	return adminFetch<{ settings: Settings }>('/settings', {
		method: 'PATCH',
		body: JSON.stringify(payload)
	});
}

// ---------------------------------------------------------------------------
// Media (Phase 4 — plan section 9 media browser / cover & OG image picking).
// Frozen contract: POST multipart "file" field, images only (jpeg/png/webp/
// gif), 10 MB default max; url like "/api/files/<id>/<name>", unauthenticated.
// ---------------------------------------------------------------------------

export interface Media {
	id: string;
	url: string;
	filename: string;
	mime: string;
	size: number;
	width: number;
	height: number;
	alt: string;
	created_at: string;
}

export interface MediaListMeta {
	page: number;
	per_page: number;
	total_items: number;
	total_pages: number;
}

export interface MediaListResult {
	media: Media[];
	meta: MediaListMeta;
}

export interface MediaListParams {
	page?: number;
	per_page?: number;
	search?: string;
}

const MEDIA_DEFAULT_PER_PAGE = 40;

export function listMedia(params: MediaListParams = {}): Promise<MediaListResult> {
	const qs = buildQuery({
		page: params.page,
		per_page: params.per_page ?? MEDIA_DEFAULT_PER_PAGE,
		search: params.search
	});
	return adminFetch<MediaListResult>(`/media${qs}`);
}

/** Uploads a single file. Callers doing multi-file uploads await these sequentially. */
export function uploadMedia(file: File): Promise<{ media: Media }> {
	const formData = new FormData();
	formData.append('file', file);
	return adminFetch<{ media: Media }>('/media', {
		method: 'POST',
		body: formData
	});
}

export function updateMediaAlt(id: string, alt: string): Promise<{ media: Media }> {
	return adminFetch<{ media: Media }>(`/media/${id}`, {
		method: 'PATCH',
		body: JSON.stringify({ alt })
	});
}

export function deleteMedia(id: string): Promise<void> {
	return adminFetch<void>(`/media/${id}`, { method: 'DELETE' });
}

// ---------------------------------------------------------------------------
// Webhooks (Phase 7, OWNER only — plan sections 8 and 9, /settings/webhooks).
// Frozen contract: the webhook secret is never returned by GET/list/PATCH —
// it is only ever present in the create and rotate-secret responses, so
// `Webhook` itself has no secret field at all.
// ---------------------------------------------------------------------------

export type WebhookEvent = 'post.published' | 'post.updated' | 'post.unpublished' | 'post.deleted';

/** Optional content filters (v0.2). Empty arrays match every post. */
export interface WebhookFilters {
	category_ids: string[];
	tag_ids: string[];
}

export interface Webhook {
	id: string;
	name: string;
	url: string;
	events: WebhookEvent[];
	filters: WebhookFilters;
	active: boolean;
	created_at: string;
}

export interface WebhookPayload {
	name: string;
	url: string;
	events: WebhookEvent[];
	filters?: WebhookFilters;
	active?: boolean;
}

export type UpdateWebhookPayload = Partial<WebhookPayload>;

export type DeliveryStatus = 'pending' | 'success' | 'failed';

export interface WebhookDelivery {
	id: string;
	event: string;
	event_id: string;
	status: DeliveryStatus;
	attempt: number;
	response_code: number | null;
	/** Truncated by the server. */
	response_body: string | null;
	next_attempt_at: string | null;
	delivered_at: string | null;
	created_at: string;
}

export interface DeliveryListMeta {
	page: number;
	per_page: number;
	total_items: number;
	total_pages: number;
}

export interface DeliveryListResult {
	deliveries: WebhookDelivery[];
	meta: DeliveryListMeta;
}

export interface DeliveryListParams {
	page?: number;
	per_page?: number;
}

const DELIVERIES_DEFAULT_PER_PAGE = 20;

export function listWebhooks(): Promise<{ webhooks: Webhook[] }> {
	return adminFetch<{ webhooks: Webhook[] }>('/webhooks');
}

/** 201 — the only other place the secret is returned is rotateWebhookSecret. */
export function createWebhook(payload: WebhookPayload): Promise<{ webhook: Webhook; secret: string }> {
	return adminFetch<{ webhook: Webhook; secret: string }>('/webhooks', {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function updateWebhook(
	id: string,
	payload: UpdateWebhookPayload
): Promise<{ webhook: Webhook }> {
	return adminFetch<{ webhook: Webhook }>(`/webhooks/${id}`, {
		method: 'PATCH',
		body: JSON.stringify(payload)
	});
}

/** 200 — invalidates the previous secret; the caller must show it exactly once. */
export function rotateWebhookSecret(id: string): Promise<{ secret: string }> {
	return adminFetch<{ secret: string }>(`/webhooks/${id}/rotate-secret`, { method: 'POST' });
}

export function deleteWebhook(id: string): Promise<void> {
	return adminFetch<void>(`/webhooks/${id}`, { method: 'DELETE' });
}

/** 202 — queues an async "webhook.test" delivery; the delivery itself shows up in listDeliveries shortly after. */
export function testWebhook(id: string): Promise<{ delivery_id: string }> {
	return adminFetch<{ delivery_id: string }>(`/webhooks/${id}/test`, { method: 'POST' });
}

export function listDeliveries(
	webhookId: string,
	params: DeliveryListParams = {}
): Promise<DeliveryListResult> {
	const qs = buildQuery({
		page: params.page,
		per_page: params.per_page ?? DELIVERIES_DEFAULT_PER_PAGE
	});
	return adminFetch<DeliveryListResult>(`/webhooks/${webhookId}/deliveries${qs}`);
}

/** 202 — allowed on failed deliveries only; the server 400s otherwise. */
export function retryDelivery(deliveryId: string): Promise<void> {
	return adminFetch<void>(`/deliveries/${deliveryId}/retry`, { method: 'POST' });
}

// ---------------------------------------------------------------------------
// API keys (v0.2, OWNER only — private content API access).
// The raw key is returned exactly once on create; list/revoke never include it.
// ---------------------------------------------------------------------------

export type APIKeyScope = 'content:read' | 'preview:read';

export interface APIKey {
	id: string;
	name: string;
	prefix: string;
	scopes: APIKeyScope[];
	expires_at: string | null;
	last_used_at: string | null;
	revoked_at: string | null;
	created_at: string;
}

export interface CreateAPIKeyPayload {
	name: string;
	scopes: APIKeyScope[];
	expires_at?: string | null;
}

export function listAPIKeys(): Promise<{ api_keys: APIKey[] }> {
	return adminFetch<{ api_keys: APIKey[] }>('/api-keys');
}

export function createAPIKey(payload: CreateAPIKeyPayload): Promise<{ api_key: APIKey; key: string }> {
	return adminFetch<{ api_key: APIKey; key: string }>('/api-keys', {
		method: 'POST',
		body: JSON.stringify(payload)
	});
}

export function revokeAPIKey(id: string): Promise<{ api_key: APIKey }> {
	return adminFetch<{ api_key: APIKey }>(`/api-keys/${id}/revoke`, { method: 'POST' });
}

export function deleteAPIKey(id: string): Promise<void> {
	return adminFetch<void>(`/api-keys/${id}`, { method: 'DELETE' });
}

import { slugify } from './slugify';
import type { PostFull, PostMediaRef, PostPayload, PostStatus } from './api';

export interface PostFormSnapshot {
	title: string;
	slug: string;
	excerpt: string;
	/** Serialized Lexical editor state JSON — the field dirty tracking keys on. */
	contentJson: string;
	status: PostStatus;
	authorId: string;
	categoryIds: string[];
	tagIds: string[];
	featured: boolean;
	locale: string;
	translateOf: string;
	seoTitle: string;
	seoDescription: string;
	canonicalUrl: string;
	/** Media id of the current cover/OG image, or "" when unset — this (not the object) is what dirty tracking and payload building key on. */
	coverImageId: string;
	ogImageId: string;
}

function sameIds(a: string[], b: string[]): boolean {
	if (a.length !== b.length) return false;
	const sortedA = [...a].sort();
	const sortedB = [...b].sort();
	return sortedA.every((id, i) => id === sortedB[i]);
}

function snapshotsEqual(a: PostFormSnapshot, b: PostFormSnapshot): boolean {
	return (
		a.title === b.title &&
		a.slug === b.slug &&
		a.excerpt === b.excerpt &&
		a.contentJson === b.contentJson &&
		a.status === b.status &&
		a.authorId === b.authorId &&
		a.featured === b.featured &&
		a.locale === b.locale &&
		a.translateOf === b.translateOf &&
		a.seoTitle === b.seoTitle &&
		a.seoDescription === b.seoDescription &&
		a.canonicalUrl === b.canonicalUrl &&
		a.coverImageId === b.coverImageId &&
		a.ogImageId === b.ogImageId &&
		sameIds(a.categoryIds, b.categoryIds) &&
		sameIds(a.tagIds, b.tagIds)
	);
}

/**
 * Editor form state for a single post (new or existing), plan section 9:
 *
 *  - "Generate slug from title only when slug is empty. Never overwrite a
 *    manually edited slug" — implemented here as setTitle()/setSlug(),
 *    mirroring the server rule client-side (the server remains authority).
 *  - "Warn before navigating away with unsaved changes" — the `dirty`
 *    getter drives the beforeNavigate/beforeunload guards in
 *    PostEditor.svelte.
 *
 * Kept as a plain class of Svelte 5 runes (same pattern as auth.svelte.ts)
 * separate from the component so both behaviors are unit-testable without
 * rendering anything — see post-editor-state.test.ts.
 */
export class PostEditorState {
	title = $state('');
	slug = $state('');
	excerpt = $state('');
	/**
	 * Serialized Lexical editor state JSON (canonical document). Dirty tracking
	 * keys on this. `contentHtml` below is derived from it by the editor and is
	 * NOT part of the dirty snapshot — it always moves together with the JSON.
	 */
	contentJson = $state('');
	/** HTML exported from the editor; sent to the server, which re-sanitizes it. */
	contentHtml = $state('');
	status = $state<PostStatus>('draft');
	/** Set once by the server when a post is first published; never cleared by unpublish. Read-only here — only status ops (Phase 6) change it. */
	publishedAt = $state<string | null>(null);
	/** The post's scheduled publish time while status is "scheduled". Read-only here — only the schedule op changes it. */
	scheduledFor = $state<string | null>(null);
	/** Server timestamps, shown as read-only meta in the editor sidebar. Not part of the dirty snapshot. */
	createdAt = $state<string | null>(null);
	updatedAt = $state<string | null>(null);
	authorId = $state('');
	categoryIds = $state<string[]>([]);
	tagIds = $state<string[]>([]);
	featured = $state(false);
	locale = $state('en');
	/** Post id to link as a translation of; "" means no link / unlink on save when changed from baseline. */
	translateOf = $state('');
	seoTitle = $state('');
	seoDescription = $state('');
	canonicalUrl = $state('');
	/** Currently selected cover/OG image, or null when unset. Assigned directly by PostEditor.svelte after a MediaPicker pick, same as categoryIds/featured. */
	coverImage = $state<PostMediaRef | null>(null);
	ogImage = $state<PostMediaRef | null>(null);

	/** True once the slug field has been edited by the user directly. */
	slugTouched = $state(false);
	/** True while this editor represents a not-yet-created post. */
	isNew = $state(true);

	#baseline: PostFormSnapshot = this.snapshot();

	snapshot(): PostFormSnapshot {
		return {
			title: this.title,
			slug: this.slug,
			excerpt: this.excerpt,
			contentJson: this.contentJson,
			status: this.status,
			authorId: this.authorId,
			categoryIds: this.categoryIds,
			tagIds: this.tagIds,
			featured: this.featured,
			locale: this.locale,
			translateOf: this.translateOf,
			seoTitle: this.seoTitle,
			seoDescription: this.seoDescription,
			canonicalUrl: this.canonicalUrl,
			coverImageId: this.coverImage?.id ?? '',
			ogImageId: this.ogImage?.id ?? ''
		};
	}

	/** True when the form no longer matches the last loaded/saved baseline. */
	get dirty(): boolean {
		return !snapshotsEqual(this.snapshot(), this.#baseline);
	}

	/**
	 * Title input handler: auto-fills the slug from the title, but only for
	 * a new post whose slug hasn't been manually edited yet.
	 */
	setTitle(value: string): void {
		this.title = value;
		if (this.isNew && !this.slugTouched) {
			this.slug = slugify(value);
		}
	}

	/**
	 * Slug input handler: any manual edit permanently stops auto-fill for
	 * this post, even if the user clears the field back to empty.
	 */
	setSlug(value: string): void {
		this.slug = value;
		this.slugTouched = true;
	}

	/**
	 * Records the editor's own canonical serialization of the *initial* document
	 * as the current value and folds it into the dirty baseline, so the untouched
	 * editor (empty for a new post, or the just-loaded document for an existing
	 * one) never reads as dirty. Lexical re-serializes with its own key order and
	 * version stamp, so the raw JSON handed to `loadFromPost` rarely matches the
	 * editor's output byte-for-byte — this reconciles the two. Call exactly once,
	 * right after the editor first mounts. Later edits go through `setContent`,
	 * which updates the value only and lets the dirty flag flip.
	 */
	commitInitialContent(json: string, html: string): void {
		this.contentJson = json;
		this.contentHtml = html;
		this.#baseline = { ...this.#baseline, contentJson: json };
	}

	/** Editor change handler: updates the live content, leaving the baseline be. */
	setContent(json: string, html: string): void {
		this.contentJson = json;
		this.contentHtml = html;
	}

	/** Resets to a blank new-post form, defaulting the author when given. */
	resetForNew(defaultAuthorId = ''): void {
		this.title = '';
		this.slug = '';
		this.excerpt = '';
		this.contentJson = '';
		this.contentHtml = '';
		this.status = 'draft';
		this.publishedAt = null;
		this.scheduledFor = null;
		this.createdAt = null;
		this.updatedAt = null;
		this.authorId = defaultAuthorId;
		this.categoryIds = [];
		this.tagIds = [];
		this.featured = false;
		this.locale = 'en';
		this.translateOf = '';
		this.seoTitle = '';
		this.seoDescription = '';
		this.canonicalUrl = '';
		this.coverImage = null;
		this.ogImage = null;
		this.slugTouched = false;
		this.isNew = true;
		this.#baseline = this.snapshot();
	}

	/**
	 * Loads an existing post's values as the new baseline. Existing posts
	 * never auto-generate their slug, so slugTouched is forced true.
	 *
	 * Also doubles as the refresh path after a status op (publish/unpublish/
	 * schedule/archive, Phase 6): those endpoints return the full updated
	 * post, and re-running the whole load here re-baselines the entire form
	 * from it — not just status/scheduled_for/published_at — so the action
	 * never leaves the form reading as dirty. See markSaved(), which is what
	 * PostEditor.svelte actually calls for both saves and status ops.
	 */
	loadFromPost(post: PostFull): void {
		this.title = post.title;
		this.slug = post.slug;
		this.excerpt = post.excerpt;
		this.contentJson = post.content_json ?? '';
		this.contentHtml = post.content_html ?? '';
		this.status = post.status;
		this.publishedAt = post.published_at;
		this.scheduledFor = post.scheduled_for;
		this.createdAt = post.created_at;
		this.updatedAt = post.updated_at;
		this.authorId = post.author.id;
		this.categoryIds = post.categories.map((category) => category.id);
		this.tagIds = post.tags.map((tag) => tag.id);
		this.featured = post.featured;
		this.locale = post.locale || 'en';
		this.translateOf = '';
		this.seoTitle = post.seo_title;
		this.seoDescription = post.seo_description;
		this.canonicalUrl = post.canonical_url;
		this.coverImage = post.cover_image;
		this.ogImage = post.og_image;
		this.slugTouched = true;
		this.isNew = false;
		this.#baseline = this.snapshot();
	}

	/**
	 * Call after a successful save, or after a status op (publish/unpublish/
	 * schedule/archive) response, so the dirty flag clears and the whole form
	 * — including status/scheduled_for/published_at — reflects the server.
	 */
	markSaved(post: PostFull): void {
		this.loadFromPost(post);
	}

	toPayload(): PostPayload {
		return {
			title: this.title,
			slug: this.slug,
			excerpt: this.excerpt,
			content_json: this.contentJson,
			content_html: this.contentHtml,
			// No `status` here — status changes only via the dedicated ops
			// (publish/unpublish/schedule/archive), never through save.
			author: this.authorId || undefined,
			categories: this.categoryIds,
			tags: this.tagIds,
			featured: this.featured,
			locale: this.locale || undefined,
			translate_of: this.translateOf.trim() === '' ? undefined : this.translateOf.trim(),
			seo_title: this.seoTitle,
			seo_description: this.seoDescription,
			canonical_url: this.canonicalUrl,
			// Always send the current value (id or "") rather than omitting when
			// untouched — simpler than tracking a third "touched" state, and
			// matches the PATCH contract (omitted = unchanged, "" = clear).
			cover_image: this.coverImage?.id ?? '',
			og_image: this.ogImage?.id ?? ''
		};
	}
}

import type { PostStatus } from './api';

/**
 * Local crash-protection autosave for the post editor (plan section 9:
 * "Autosave locally in the browser for crash protection. Server save remains
 * explicit."). The editor debounces a snapshot of the whole form into
 * localStorage; on reopen we decide whether a saved snapshot is worth
 * restoring. This module is deliberately pure (no Svelte runes, no direct
 * localStorage access beyond the injected `Storage`) so the compare/restore
 * logic is unit-testable — see post-draft.test.ts.
 */

/** The full editor form, exactly the fields we round-trip through localStorage. */
export interface DraftForm {
	title: string;
	slug: string;
	excerpt: string;
	contentJson: string;
	contentHtml: string;
	status: PostStatus;
	authorId: string;
	categoryIds: string[];
	tagIds: string[];
	featured: boolean;
	seoTitle: string;
	seoDescription: string;
	canonicalUrl: string;
	metadataText: string;
}

/** A stored draft: the form plus the moment it was captured. */
export interface DraftSnapshot extends DraftForm {
	/** ISO-8601 timestamp of when this snapshot was written. */
	savedAt: string;
}

/** localStorage key for a post's local draft — new posts share the `new` slot. */
export function draftKey(postId?: string | null): string {
	return `pp-draft-${postId ?? 'new'}`;
}

function sameIds(a: string[], b: string[]): boolean {
	if (a.length !== b.length) return false;
	const sortedA = [...a].sort();
	const sortedB = [...b].sort();
	return sortedA.every((id, i) => id === sortedB[i]);
}

/** Deep-equal over the form fields (order-insensitive for category/tag ids). */
export function draftFormsEqual(a: DraftForm, b: DraftForm): boolean {
	return (
		a.title === b.title &&
		a.slug === b.slug &&
		a.excerpt === b.excerpt &&
		a.contentJson === b.contentJson &&
		a.status === b.status &&
		a.authorId === b.authorId &&
		a.featured === b.featured &&
		a.seoTitle === b.seoTitle &&
		a.seoDescription === b.seoDescription &&
		a.canonicalUrl === b.canonicalUrl &&
		a.metadataText === b.metadataText &&
		sameIds(a.categoryIds, b.categoryIds) &&
		sameIds(a.tagIds, b.tagIds)
	);
}

/** Reads and parses a stored draft, tolerating missing/corrupt entries. */
export function readDraft(storage: Storage, key: string): DraftSnapshot | null {
	let raw: string | null = null;
	try {
		raw = storage.getItem(key);
	} catch {
		return null;
	}
	if (!raw) return null;
	try {
		const parsed = JSON.parse(raw) as Partial<DraftSnapshot>;
		if (!parsed || typeof parsed !== 'object' || typeof parsed.savedAt !== 'string') {
			return null;
		}
		return parsed as DraftSnapshot;
	} catch {
		return null;
	}
}

/** Serializes a snapshot into storage; failures (quota, private mode) are ignored. */
export function writeDraft(storage: Storage, key: string, snapshot: DraftSnapshot): void {
	try {
		storage.setItem(key, JSON.stringify(snapshot));
	} catch {
		// best-effort — a full or unavailable store just means no crash protection.
	}
}

/** Removes a stored draft (after an explicit server save or a discard). */
export function clearDraft(storage: Storage, key: string): void {
	try {
		storage.removeItem(key);
	} catch {
		// ignore
	}
}

export interface RestoreDecisionInput {
	/** The draft found in localStorage, if any. */
	local: DraftSnapshot | null;
	/** The current server-side form (canonically serialized), or null for a new post. */
	server: DraftForm | null;
	/** The server post's `updated_at`, or null for a new post. */
	serverUpdatedAt: string | null;
}

/**
 * Decides whether to offer the "Unsaved local draft found" bar. Restore when a
 * draft exists, actually differs from the server copy, and is newer than the
 * server's last save (plan section 9). A new post (no server copy yet) always
 * offers its draft. An unparseable `savedAt`/`updated_at` errs toward offering
 * the draft rather than silently dropping the user's work.
 */
export function shouldRestoreDraft({
	local,
	server,
	serverUpdatedAt
}: RestoreDecisionInput): boolean {
	if (!local) return false;
	// New post: any saved draft is unsaved work worth restoring.
	if (!server) return true;
	// Identical to what the server already has — nothing to restore.
	if (draftFormsEqual(local, server)) return false;
	// Stale: the server was saved at or after this draft was captured.
	if (serverUpdatedAt) {
		const draftTime = Date.parse(local.savedAt);
		const serverTime = Date.parse(serverUpdatedAt);
		if (!Number.isNaN(draftTime) && !Number.isNaN(serverTime) && draftTime <= serverTime) {
			return false;
		}
	}
	return true;
}

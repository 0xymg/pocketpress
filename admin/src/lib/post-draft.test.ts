import { describe, expect, it } from 'vitest';
import {
	clearDraft,
	draftFormsEqual,
	draftKey,
	readDraft,
	shouldRestoreDraft,
	writeDraft,
	type DraftForm,
	type DraftSnapshot
} from './post-draft';

/** Minimal in-memory Storage for exercising the read/write/clear helpers. */
function fakeStorage(seed: Record<string, string> = {}): Storage {
	const map = new Map(Object.entries(seed));
	return {
		get length() {
			return map.size;
		},
		clear: () => map.clear(),
		getItem: (k: string) => (map.has(k) ? (map.get(k) as string) : null),
		key: (i: number) => Array.from(map.keys())[i] ?? null,
		removeItem: (k: string) => map.delete(k),
		setItem: (k: string, v: string) => map.set(k, v)
	} as Storage;
}

function form(overrides: Partial<DraftForm> = {}): DraftForm {
	return {
		title: 'Title',
		slug: 'title',
		excerpt: '',
		contentJson: '{"root":{"children":[]}}',
		contentHtml: '<p></p>',
		status: 'draft',
		authorId: 'author-1',
		categoryIds: ['cat-1', 'cat-2'],
		tagIds: [],
		featured: false,
		seoTitle: '',
		seoDescription: '',
		canonicalUrl: '',
		metadataText: '{}',
		...overrides
	};
}

function snapshot(overrides: Partial<DraftSnapshot> = {}): DraftSnapshot {
	return { ...form(), savedAt: '2024-02-01T00:00:00Z', ...overrides };
}

describe('draftKey', () => {
	it('namespaces by post id and shares one slot for new posts', () => {
		expect(draftKey('abc')).toBe('pp-draft-abc');
		expect(draftKey(undefined)).toBe('pp-draft-new');
		expect(draftKey(null)).toBe('pp-draft-new');
	});
});

describe('draftFormsEqual', () => {
	it('ignores category/tag ordering', () => {
		expect(
			draftFormsEqual(form({ categoryIds: ['a', 'b'] }), form({ categoryIds: ['b', 'a'] }))
		).toBe(true);
	});

	it('detects a content change', () => {
		expect(draftFormsEqual(form(), form({ contentJson: '{"root":{"children":[1]}}' }))).toBe(false);
	});

	it('detects a scalar field change', () => {
		expect(draftFormsEqual(form(), form({ title: 'Different' }))).toBe(false);
	});
});

describe('readDraft / writeDraft / clearDraft', () => {
	it('round-trips a snapshot through storage', () => {
		const storage = fakeStorage();
		const snap = snapshot();
		writeDraft(storage, 'pp-draft-new', snap);
		expect(readDraft(storage, 'pp-draft-new')).toEqual(snap);
	});

	it('returns null for a missing key', () => {
		expect(readDraft(fakeStorage(), 'pp-draft-new')).toBeNull();
	});

	it('returns null for corrupt JSON', () => {
		expect(readDraft(fakeStorage({ 'pp-draft-new': 'not json' }), 'pp-draft-new')).toBeNull();
	});

	it('returns null when savedAt is missing', () => {
		expect(
			readDraft(fakeStorage({ 'pp-draft-new': JSON.stringify({ title: 'x' }) }), 'pp-draft-new')
		).toBeNull();
	});

	it('clears a stored draft', () => {
		const storage = fakeStorage({ 'pp-draft-new': JSON.stringify(snapshot()) });
		clearDraft(storage, 'pp-draft-new');
		expect(readDraft(storage, 'pp-draft-new')).toBeNull();
	});
});

describe('shouldRestoreDraft', () => {
	it('does not restore when there is no local draft', () => {
		expect(shouldRestoreDraft({ local: null, server: form(), serverUpdatedAt: null })).toBe(false);
	});

	it('restores any saved draft for a new post (no server copy)', () => {
		expect(shouldRestoreDraft({ local: snapshot(), server: null, serverUpdatedAt: null })).toBe(
			true
		);
	});

	it('does not restore when the draft matches the server copy', () => {
		expect(
			shouldRestoreDraft({
				local: snapshot(),
				server: form(),
				serverUpdatedAt: '2024-01-01T00:00:00Z'
			})
		).toBe(false);
	});

	it('restores a differing draft that is newer than the server save', () => {
		expect(
			shouldRestoreDraft({
				local: snapshot({ title: 'Newer local edit', savedAt: '2024-03-01T00:00:00Z' }),
				server: form(),
				serverUpdatedAt: '2024-01-01T00:00:00Z'
			})
		).toBe(true);
	});

	it('discards a stale draft older than the server save', () => {
		expect(
			shouldRestoreDraft({
				local: snapshot({ title: 'Old local edit', savedAt: '2024-01-01T00:00:00Z' }),
				server: form(),
				serverUpdatedAt: '2024-02-15T00:00:00Z'
			})
		).toBe(false);
	});
});

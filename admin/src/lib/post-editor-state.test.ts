import { describe, expect, it } from 'vitest';
import { PostEditorState } from './post-editor-state.svelte';
import type { PostFull } from './api';

function fakePost(overrides: Partial<PostFull> = {}): PostFull {
	return {
		id: 'post-1',
		title: 'Existing title',
		slug: 'existing-title',
		status: 'draft',
		excerpt: 'An excerpt',
		featured: false,
		locale: 'en',
		author: { id: 'author-1', name: 'Ada Lovelace', slug: 'ada-lovelace' },
		categories: [{ id: 'cat-1', name: 'News', slug: 'news' }],
		tags: [{ id: 'tag-1', name: 'Launch', slug: 'launch' }],
		scheduled_for: null,
		published_at: null,
		created_at: '2024-01-01T00:00:00Z',
		updated_at: '2024-01-02T00:00:00Z',
		content_json: '{"root":{"children":[],"type":"root","version":1}}',
		content_html: '<p>Body text</p>',
		cover_image: null,
		og_image: null,
		seo_title: '',
		seo_description: '',
		canonical_url: '',
		translation_group: null,
		translations: [],
		...overrides
	};
}

describe('PostEditorState — auto-slug behavior', () => {
	it('fills the slug from the title while the post is new and the slug is untouched', () => {
		const state = new PostEditorState();
		state.resetForNew();

		state.setTitle('Hello World');

		expect(state.title).toBe('Hello World');
		expect(state.slug).toBe('hello-world');
	});

	it('keeps auto-filling as the title keeps changing, until the slug is edited', () => {
		const state = new PostEditorState();
		state.resetForNew();

		state.setTitle('First Draft Title');
		expect(state.slug).toBe('first-draft-title');

		state.setTitle('Second Draft Title');
		expect(state.slug).toBe('second-draft-title');
	});

	it('stops touching the slug once the user edits it manually, even if they clear it', () => {
		const state = new PostEditorState();
		state.resetForNew();

		state.setTitle('Original Title');
		expect(state.slug).toBe('original-title');

		state.setSlug('my-custom-slug');
		state.setTitle('Changed Title');
		expect(state.slug).toBe('my-custom-slug');

		state.setSlug('');
		state.setTitle('Changed Again');
		expect(state.slug).toBe('');
	});

	it('never auto-fills the slug for an existing post', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost({ slug: 'existing-title' }));

		state.setTitle('A Brand New Headline');

		expect(state.title).toBe('A Brand New Headline');
		expect(state.slug).toBe('existing-title');
	});
});

describe('PostEditorState — dirty tracking', () => {
	it('is not dirty right after resetting for a new post', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');

		expect(state.dirty).toBe(false);
	});

	it('becomes dirty as soon as a field changes', () => {
		const state = new PostEditorState();
		state.resetForNew();

		state.setTitle('Draft title');

		expect(state.dirty).toBe(true);
	});

	it('is not dirty right after loading an existing post', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost());

		expect(state.dirty).toBe(false);
	});

	it('treats category/tag selection changes as dirty regardless of order', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost({ categories: [], tags: [] }));
		expect(state.dirty).toBe(false);

		state.categoryIds = ['cat-1', 'cat-2'];
		expect(state.dirty).toBe(true);
	});

	it('clears dirty after markSaved, using the server response as the new baseline', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');
		state.setTitle('New Post');
		expect(state.dirty).toBe(true);

		state.markSaved(fakePost({ title: 'New Post', slug: 'new-post' }));

		expect(state.dirty).toBe(false);
		expect(state.isNew).toBe(false);
	});

	it('becomes dirty again after a save if the user keeps editing', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost());
		expect(state.dirty).toBe(false);

		state.excerpt = 'Updated excerpt';

		expect(state.dirty).toBe(true);
	});
});

describe('PostEditorState — payload building', () => {
	it('serializes to the PostPayload shape the API expects, with no status field', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');
		state.setTitle('My Post');
		state.categoryIds = ['cat-1'];
		state.tagIds = ['tag-1'];
		state.featured = true;

		expect(state.toPayload()).toEqual({
			title: 'My Post',
			slug: 'my-post',
			excerpt: '',
			content_json: '',
			content_html: '',
			author: 'author-1',
			categories: ['cat-1'],
			tags: ['tag-1'],
			featured: true,
			locale: 'en',
			seo_title: '',
			seo_description: '',
			canonical_url: '',
			cover_image: '',
			og_image: ''
		});
	});

	it('never includes status in the payload, even after loading a non-draft post', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost({ status: 'published', published_at: '2026-01-01T00:00:00Z' }));

		expect(state.toPayload()).not.toHaveProperty('status');
	});

	it('carries content_json and content_html through to the payload', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');
		state.setContent('{"root":{"children":[1]}}', '<p>Hi</p>');

		const payload = state.toPayload();
		expect(payload.content_json).toBe('{"root":{"children":[1]}}');
		expect(payload.content_html).toBe('<p>Hi</p>');
	});
});

describe('PostEditorState — rich-text content dirty tracking', () => {
	it('flips dirty when the editor reports new content JSON', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');
		state.commitInitialContent('{"root":{"children":[]}}', '<p></p>');
		expect(state.dirty).toBe(false);

		state.setContent('{"root":{"children":[1]}}', '<p>New</p>');
		expect(state.dirty).toBe(true);
	});

	it('commitInitialContent folds the editor serialization into the baseline (not dirty)', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost({ content_json: '{"raw":true}' }));

		// The live editor re-serializes to its own canonical JSON on mount.
		state.commitInitialContent('{"root":{"children":[7]}}', '<p>Body</p>');

		expect(state.contentJson).toBe('{"root":{"children":[7]}}');
		expect(state.dirty).toBe(false);
	});

	it('setContent does not touch the baseline, so an equal re-serialization is clean', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');
		state.commitInitialContent('{"root":{"children":[]}}', '<p></p>');

		// The debounced onChange fires with the same JSON — should stay clean.
		state.setContent('{"root":{"children":[]}}', '<p></p>');
		expect(state.dirty).toBe(false);
	});
});

describe('PostEditorState — cover/OG image', () => {
	const PICKED_MEDIA = {
		id: 'media-1',
		url: '/api/files/media-1/cover.jpg',
		alt: 'A cover shot',
		width: 800,
		height: 600
	};

	it('is not dirty right after loading a post with no cover/OG image', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost());

		expect(state.dirty).toBe(false);
		expect(state.coverImage).toBeNull();
		expect(state.ogImage).toBeNull();
	});

	it('becomes dirty when a cover image is picked, and its id lands in the payload', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost());

		state.coverImage = PICKED_MEDIA;

		expect(state.dirty).toBe(true);
		expect(state.toPayload().cover_image).toBe('media-1');
		expect(state.toPayload().og_image).toBe('');
	});

	it('sends an empty string when a previously-set cover image is cleared', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost({ cover_image: PICKED_MEDIA }));
		expect(state.dirty).toBe(false);

		state.coverImage = null;

		expect(state.dirty).toBe(true);
		expect(state.toPayload().cover_image).toBe('');
	});

	it('tracks the OG image independently of the cover image', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');

		state.ogImage = { ...PICKED_MEDIA, id: 'media-2' };

		expect(state.dirty).toBe(true);
		const payload = state.toPayload();
		expect(payload.og_image).toBe('media-2');
		expect(payload.cover_image).toBe('');
	});

	it('clears dirty and re-baselines both images after markSaved', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');
		state.coverImage = PICKED_MEDIA;
		expect(state.dirty).toBe(true);

		state.markSaved(fakePost({ cover_image: PICKED_MEDIA }));

		expect(state.dirty).toBe(false);
		expect(state.coverImage).toEqual(PICKED_MEDIA);
	});
});

describe('PostEditorState — status/scheduled_for/published_at (Phase 6 status ops)', () => {
	it('starts a new post as draft with no scheduled/published time', () => {
		const state = new PostEditorState();
		state.resetForNew('author-1');

		expect(state.status).toBe('draft');
		expect(state.scheduledFor).toBeNull();
		expect(state.publishedAt).toBeNull();
	});

	it('loads status/scheduled_for/published_at from an existing post', () => {
		const state = new PostEditorState();
		state.loadFromPost(
			fakePost({ status: 'scheduled', scheduled_for: '2026-09-01T10:00:00Z', published_at: null })
		);

		expect(state.status).toBe('scheduled');
		expect(state.scheduledFor).toBe('2026-09-01T10:00:00Z');
		expect(state.publishedAt).toBeNull();
	});

	it('refreshes status/scheduled_for/published_at and clears dirty via markSaved, as used after a status op', () => {
		const state = new PostEditorState();
		state.loadFromPost(fakePost({ status: 'draft' }));
		expect(state.dirty).toBe(false);

		// Simulates PostEditor.svelte calling formState.markSaved(post) with the
		// full post returned by e.g. POST /posts/:id/publish.
		state.markSaved(
			fakePost({ status: 'published', published_at: '2026-08-02T12:00:00Z', scheduled_for: null })
		);

		expect(state.status).toBe('published');
		expect(state.publishedAt).toBe('2026-08-02T12:00:00Z');
		expect(state.scheduledFor).toBeNull();
		expect(state.dirty).toBe(false);
	});

	it('preserves published_at across an unpublish (back to draft) refresh', () => {
		const state = new PostEditorState();
		state.loadFromPost(
			fakePost({ status: 'published', published_at: '2026-08-02T12:00:00Z', scheduled_for: null })
		);

		// Simulates the response from POST /posts/:id/unpublish: status flips
		// back to draft, but published_at is preserved server-side.
		state.markSaved(
			fakePost({ status: 'draft', published_at: '2026-08-02T12:00:00Z', scheduled_for: null })
		);

		expect(state.status).toBe('draft');
		expect(state.publishedAt).toBe('2026-08-02T12:00:00Z');
	});
});

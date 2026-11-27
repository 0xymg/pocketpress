import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const gotoMock = vi.fn();
vi.mock('$app/navigation', () => ({
	goto: (...args: unknown[]) => gotoMock(...args),
	beforeNavigate: () => {}
}));

import { auth } from '$lib/auth.svelte';
import PostEditor from './PostEditor.svelte';

const OWNER = {
	id: 'author-1',
	name: 'Ada Lovelace',
	email: 'ada@example.com',
	role: 'owner' as const,
	slug: 'ada-lovelace',
	bio: '',
	website: '',
	location: '',
	avatar: '',
	active: true,
	last_login_at: null,
	created_at: '2024-01-01T00:00:00Z'
};

function jsonResponse(status: number, ok: boolean, body: unknown) {
	return { ok, status, text: async () => JSON.stringify(body) };
}

const REFERENCE_BODIES = {
	categories: {
		categories: [
			{
				id: 'cat-1',
				name: 'News',
				slug: 'news',
				description: null,
				created_at: '2024-01-01T00:00:00Z',
				updated_at: '2024-01-01T00:00:00Z'
			}
		]
	},
	tags: {
		tags: [
			{ id: 'tag-1', name: 'Launch', slug: 'launch', created_at: '2024-01-01T00:00:00Z', updated_at: '2024-01-01T00:00:00Z' }
		]
	},
	authors: { authors: [{ id: 'author-1', name: 'Ada Lovelace', slug: 'ada-lovelace' }] }
};

const EXISTING_POST = {
	id: 'post-1',
	title: 'Existing Post',
	slug: 'existing-post',
	status: 'draft' as const,
	excerpt: 'An excerpt',
	featured: false,
	locale: 'en',
	author: { id: 'author-1', name: 'Ada Lovelace', slug: 'ada-lovelace' },
	categories: [],
	tags: [],
	scheduled_for: null,
	published_at: null,
	review_status: 'none' as const,
	review_note: '',
	reviewed_by: null,
	reviewed_at: null,
	submitted_for_review_at: null,
	created_at: '2024-01-01T00:00:00Z',
	updated_at: '2024-01-02T00:00:00Z',
	content_json: '{"root":{"children":[{"children":[{"detail":0,"format":0,"mode":"normal","style":"","text":"Body text","type":"text","version":1}],"direction":"ltr","format":"","indent":0,"type":"paragraph","version":1}],"direction":"ltr","format":"","indent":0,"type":"root","version":1}}',
	content_html: '<p>Body text</p>',
	cover_image: null,
	og_image: null,
	seo_title: '',
	seo_description: '',
	canonical_url: '',
	translation_group: null,
	translations: [],
	metadata: {}
};

/** Same document, but scheduled — used for the "scheduled" action-bar and chip tests. */
const SCHEDULED_POST = {
	...EXISTING_POST,
	id: 'post-2',
	status: 'scheduled' as const,
	scheduled_for: '2026-08-10T15:30:00Z',
	published_at: null
};

/** Same document, but published — used for the "published" action-bar and chip tests. */
const PUBLISHED_POST = {
	...EXISTING_POST,
	id: 'post-3',
	status: 'published' as const,
	scheduled_for: null,
	published_at: '2026-07-01T09:00:00Z'
};

/** Same document, but archived — used for the "archived" action-bar (Restore to draft, Delete only) tests. */
const ARCHIVED_POST = {
	...EXISTING_POST,
	id: 'post-4',
	status: 'archived' as const,
	scheduled_for: null,
	published_at: null
};

/** Routes fetch requests by "METHOD /path" so multiple endpoints (fired
 * concurrently by loadReferenceData) can be mocked from one fetch stub. */
function routedFetch(routes: Record<string, () => ReturnType<typeof jsonResponse>>) {
	return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === 'string' ? input : input.toString();
		const method = (init?.method ?? 'GET').toUpperCase();
		const key = `${method} ${url}`;
		const handler = routes[key];
		if (!handler) {
			throw new Error(`Unhandled request in test: ${key}`);
		}
		return handler();
	});
}

function withReferenceRoutes(routes: Record<string, () => ReturnType<typeof jsonResponse>>) {
	return {
		'GET /api/admin/v1/categories': () => jsonResponse(200, true, REFERENCE_BODIES.categories),
		'GET /api/admin/v1/tags': () => jsonResponse(200, true, REFERENCE_BODIES.tags),
		'GET /api/admin/v1/authors': () => jsonResponse(200, true, REFERENCE_BODIES.authors),
		'GET /api/admin/v1/settings': () =>
			jsonResponse(200, true, {
				settings: {
					site_name: 'PocketPress',
					site_url: 'https://example.com',
					admin_title: 'PocketPress',
					locale: 'en',
					available_locales: ['en'],
					timezone: 'UTC',
					default_author: null,
					cors_origins: [],
					public_api_enabled: true,
					preview_url_template: null,
					webhook_user_agent: 'PocketPress',
					require_review_before_publish: false
				}
			}),
		...routes
	};
}

/** Opens the doc-controls "..." popup menu that holds the secondary actions. */
async function openMenu() {
	await fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
}

describe('PostEditor — new post', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('auto-fills the slug from the title, then stops once the slug is edited manually', async () => {
		vi.stubGlobal('fetch', routedFetch(withReferenceRoutes({})));

		render(PostEditor);

		const titleInput = screen.getByLabelText('Title');
		const slugInput = screen.getByLabelText('Slug');

		await fireEvent.input(titleInput, { target: { value: 'My First Post' } });
		await waitFor(() => {
			expect((slugInput as HTMLInputElement).value).toBe('my-first-post');
		});

		await fireEvent.input(slugInput, { target: { value: 'custom-slug' } });
		await fireEvent.input(titleInput, { target: { value: 'My First Post, Revised' } });

		await waitFor(() => {
			expect((slugInput as HTMLInputElement).value).toBe('custom-slug');
		});
	});

	it('renders validation_failed field messages next to title and slug', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'POST /api/admin/v1/posts': () =>
						jsonResponse(400, false, {
							error: {
								code: 'validation_failed',
								message: 'Validation failed',
								details: {
									title: 'Title is required',
									slug: 'Slug is already taken'
								}
							}
						})
				})
			)
		);

		render(PostEditor);

		await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

		await waitFor(() => {
			expect(screen.getByText('Title is required')).toBeInTheDocument();
		});
		expect(screen.getByText('Slug is already taken')).toBeInTheDocument();
		expect(gotoMock).not.toHaveBeenCalled();
	});

	it('creates the post, shows a Saved timestamp, and navigates to the new post URL', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'POST /api/admin/v1/posts': () =>
						jsonResponse(201, true, { post: { ...EXISTING_POST, id: 'post-2' } })
				})
			)
		);

		render(PostEditor);

		await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Brand New Post' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

		await waitFor(() => {
			expect(gotoMock).toHaveBeenCalledWith('/posts/post-2', { replaceState: true });
		});
		expect(screen.getByText(/^Saved \d{2}:\d{2}$/)).toBeInTheDocument();
	});

	it('sends no status field on create — status is server-defaulted, never part of the save payload', async () => {
		const fetchMock = routedFetch(
			withReferenceRoutes({
				'POST /api/admin/v1/posts': () =>
					jsonResponse(201, true, { post: { ...EXISTING_POST, id: 'post-2' } })
			})
		);
		vi.stubGlobal('fetch', fetchMock);

		render(PostEditor);

		await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Brand New Post' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

		await waitFor(() => {
			expect(gotoMock).toHaveBeenCalled();
		});

		const createCall = fetchMock.mock.calls.find(
			([url, init]) =>
				String(url) === '/api/admin/v1/posts' &&
				(init as RequestInit | undefined)?.method === 'POST'
		);
		expect(createCall).toBeDefined();
		const body = JSON.parse(String((createCall?.[1] as RequestInit).body));
		expect(body).not.toHaveProperty('status');
	});

	it('shows the current-user default author and the SEO title character counter', async () => {
		vi.stubGlobal('fetch', routedFetch(withReferenceRoutes({})));

		render(PostEditor);

		await waitFor(() => {
			expect(screen.getByText('Ada Lovelace')).toBeInTheDocument();
		});
		expect(screen.getByText('0/70')).toBeInTheDocument();

		await fireEvent.input(screen.getByLabelText('SEO title'), { target: { value: 'Hello' } });
		await waitFor(() => {
			expect(screen.getByText('5/70')).toBeInTheDocument();
		});
	});

	it('has no status action bar for a new, unsaved post', async () => {
		vi.stubGlobal('fetch', routedFetch(withReferenceRoutes({})));

		render(PostEditor);

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
		});
		expect(screen.queryByRole('button', { name: 'Publish' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Preview' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Delete' })).not.toBeInTheDocument();
	});
});

describe('PostEditor — existing post (draft)', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('loads the post and renders its fields', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST })
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Existing Post');
		});
		expect((screen.getByLabelText('Slug') as HTMLInputElement).value).toBe('existing-post');
	});

	it('does not auto-fill the slug from the title once a post already exists', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST })
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Existing Post');
		});

		await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Retitled Post' } });

		expect((screen.getByLabelText('Slug') as HTMLInputElement).value).toBe('existing-post');
	});

	it('picks a cover image via the media picker and saves it with the post', async () => {
		const MEDIA_ITEM = {
			id: 'media-1',
			url: '/api/files/media-1/cover.jpg',
			filename: 'cover.jpg',
			mime: 'image/jpeg',
			size: 2048,
			width: 800,
			height: 600,
			alt: 'A cover shot',
			created_at: '2024-01-01T00:00:00Z'
		};
		const fetchMock = routedFetch(
			withReferenceRoutes({
				'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST }),
				'GET /api/admin/v1/media?page=1&per_page=40': () =>
					jsonResponse(200, true, {
						media: [MEDIA_ITEM],
						meta: { page: 1, per_page: 40, total_items: 1, total_pages: 1 }
					}),
				'PATCH /api/admin/v1/posts/post-1': () =>
					jsonResponse(200, true, {
						post: {
							...EXISTING_POST,
							cover_image: {
								id: 'media-1',
								url: '/api/files/media-1/cover.jpg',
								alt: 'A cover shot',
								width: 800,
								height: 600
							}
						}
					})
			})
		);
		vi.stubGlobal('fetch', fetchMock);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Pick cover image' })).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Pick cover image' }));

		await waitFor(() => {
			expect(screen.getByRole('dialog', { name: 'Select media' })).toBeInTheDocument();
		});
		await waitFor(() => {
			expect(screen.getByText('cover.jpg')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByText('cover.jpg'));

		await waitFor(() => {
			expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
		});
		expect(screen.getByRole('button', { name: 'Change cover image' })).toBeInTheDocument();

		await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

		await waitFor(() => {
			expect(screen.getByText(/^Saved \d{2}:\d{2}$/)).toBeInTheDocument();
		});

		const patchCall = fetchMock.mock.calls.find(
			([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
		);
		expect(patchCall).toBeDefined();
		const body = JSON.parse(String((patchCall?.[1] as RequestInit).body));
		expect(body.cover_image).toBe('media-1');
	});
});

describe('PostEditor — status action bar (Phase 6, per status)', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('draft: shows Save, Publish, Preview + a menu with Schedule…/Archive/Delete — no Unpublish/Back to draft/Restore', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST })
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect(screen.getByText('draft')).toBeInTheDocument();
		});
		expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Publish' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Preview' })).toBeInTheDocument();
		// Secondary actions live in the "..." menu.
		await openMenu();
		expect(screen.getByRole('button', { name: 'Schedule…' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Archive' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Unpublish' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Back to draft' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Restore to draft' })).not.toBeInTheDocument();
	});

	it('scheduled: shows the "Scheduled for …" chip and Publish now/Edit schedule…/Back to draft/Archive/Preview', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-2': () => jsonResponse(200, true, { post: SCHEDULED_POST })
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-2' } });

		await waitFor(() => {
			expect(screen.getByText(/^Scheduled for/)).toBeInTheDocument();
		});
		expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Preview' })).toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Publish' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Restore to draft' })).not.toBeInTheDocument();
		await openMenu();
		expect(screen.getByRole('button', { name: 'Publish now' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Edit schedule…' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Back to draft' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Archive' })).toBeInTheDocument();
	});

	it('published: shows the "Published …" chip and Unpublish/Archive/Preview', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-3': () => jsonResponse(200, true, { post: PUBLISHED_POST })
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-3' } });

		await waitFor(() => {
			expect(screen.getByText(/^Published/)).toBeInTheDocument();
		});
		expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Preview' })).toBeInTheDocument();
		await openMenu();
		expect(screen.getByRole('button', { name: 'Unpublish' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Archive' })).toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Schedule…' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Back to draft' })).not.toBeInTheDocument();
	});

	it('archived: shows only Restore to draft and Delete — no Save/Preview/Schedule/Publish/Archive/Unpublish', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-4': () => jsonResponse(200, true, { post: ARCHIVED_POST })
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-4' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Restore to draft' })).toBeInTheDocument();
		});
		expect(screen.queryByRole('button', { name: 'Save' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Preview' })).not.toBeInTheDocument();
		await openMenu();
		expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Duplicate' })).toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Archive' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Unpublish' })).not.toBeInTheDocument();
		expect(screen.queryByRole('button', { name: 'Publish' })).not.toBeInTheDocument();
	});

	it('deletes an archived post on confirm and navigates back to the list', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-4': () => jsonResponse(200, true, { post: ARCHIVED_POST }),
					'DELETE /api/admin/v1/posts/post-4': () => jsonResponse(204, true, null)
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-4' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Restore to draft' })).toBeInTheDocument();
		});

		await openMenu();
		await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm delete' }));

		await waitFor(() => {
			expect(gotoMock).toHaveBeenCalledWith('/posts');
		});
	});
});

describe('PostEditor — publish flow', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('clicking Publish asks "Publish now?"; Confirm calls the publish endpoint and updates the status', async () => {
		const fetchMock = routedFetch(
			withReferenceRoutes({
				'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST }),
				'POST /api/admin/v1/posts/post-1/publish': () =>
					jsonResponse(200, true, {
						post: { ...EXISTING_POST, status: 'published', published_at: '2026-08-02T12:00:00Z' }
					})
			})
		);
		vi.stubGlobal('fetch', fetchMock);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Publish' })).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Publish' }));
		expect(screen.getByText('Publish now?')).toBeInTheDocument();

		await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

		await waitFor(() => {
			expect(screen.getByText('published')).toBeInTheDocument();
		});
		expect(screen.getByText(/^Published/)).toBeInTheDocument();

		const publishCall = fetchMock.mock.calls.find(
			([url, init]) =>
				String(url) === '/api/admin/v1/posts/post-1/publish' &&
				(init as RequestInit | undefined)?.method === 'POST'
		);
		expect(publishCall).toBeDefined();
	});

	it('"Publish now" on a scheduled post publishes immediately without a confirm step', async () => {
		const fetchMock = routedFetch(
			withReferenceRoutes({
				'GET /api/admin/v1/posts/post-2': () => jsonResponse(200, true, { post: SCHEDULED_POST }),
				'POST /api/admin/v1/posts/post-2/publish': () =>
					jsonResponse(200, true, {
						post: {
							...SCHEDULED_POST,
							status: 'published',
							scheduled_for: null,
							published_at: '2026-08-02T12:00:00Z'
						}
					})
			})
		);
		vi.stubGlobal('fetch', fetchMock);

		render(PostEditor, { props: { postId: 'post-2' } });

		await waitFor(() => {
			expect(screen.getByText(/^Scheduled for/)).toBeInTheDocument();
		});
		await openMenu();
		await fireEvent.click(screen.getByRole('button', { name: 'Publish now' }));

		await waitFor(() => {
			expect(screen.getByText('published')).toBeInTheDocument();
		});

		const publishCall = fetchMock.mock.calls.find(
			([url, init]) =>
				String(url) === '/api/admin/v1/posts/post-2/publish' &&
				(init as RequestInit | undefined)?.method === 'POST'
		);
		expect(publishCall).toBeDefined();
	});

	it('Unpublish (from published) preserves published_at across the badge refresh', async () => {
		const fetchMock = routedFetch(
			withReferenceRoutes({
				'GET /api/admin/v1/posts/post-3': () => jsonResponse(200, true, { post: PUBLISHED_POST }),
				'POST /api/admin/v1/posts/post-3/unpublish': () =>
					jsonResponse(200, true, {
						post: { ...PUBLISHED_POST, status: 'draft' }
					})
			})
		);
		vi.stubGlobal('fetch', fetchMock);

		render(PostEditor, { props: { postId: 'post-3' } });

		await waitFor(() => {
			expect(screen.getByText(/^Published/)).toBeInTheDocument();
		});
		await openMenu();
		await fireEvent.click(screen.getByRole('button', { name: 'Unpublish' }));

		await waitFor(() => {
			expect(screen.getByText('draft')).toBeInTheDocument();
		});
		expect(screen.getByRole('button', { name: 'Publish' })).toBeInTheDocument();
	});
});

describe('PostEditor — schedule flow', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('rejects a past date/time client-side, without calling the schedule endpoint', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST })
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
		});
		await openMenu();
		await fireEvent.click(screen.getByRole('button', { name: 'Schedule…' }));

		const input = screen.getByLabelText('Scheduled for');
		await fireEvent.input(input, { target: { value: '2000-01-01T10:00' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

		await waitFor(() => {
			expect(screen.getByText('Scheduled time must be in the future.')).toBeInTheDocument();
		});
		// The unhandled-route guard in routedFetch would have thrown had the
		// schedule endpoint been called — reaching here proves it wasn't.
	});

	it('renders a server 400 validation_failed details.scheduled_for message', async () => {
		const fetchMock = routedFetch(
			withReferenceRoutes({
				'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST }),
				'POST /api/admin/v1/posts/post-1/schedule': () =>
					jsonResponse(400, false, {
						error: {
							code: 'validation_failed',
							message: 'Validation failed',
							details: { scheduled_for: 'That time has already passed.' }
						}
					})
			})
		);
		vi.stubGlobal('fetch', fetchMock);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
		});
		await openMenu();
		await fireEvent.click(screen.getByRole('button', { name: 'Schedule…' }));

		const future = new Date(Date.now() + 2 * 60 * 60 * 1000);
		const pad = (n: number) => String(n).padStart(2, '0');
		const futureValue = `${future.getFullYear()}-${pad(future.getMonth() + 1)}-${pad(future.getDate())}T${pad(future.getHours())}:${pad(future.getMinutes())}`;

		await fireEvent.input(screen.getByLabelText('Scheduled for'), {
			target: { value: futureValue }
		});
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

		await waitFor(() => {
			expect(screen.getByText('That time has already passed.')).toBeInTheDocument();
		});

		const scheduleCall = fetchMock.mock.calls.find(
			([url, init]) =>
				String(url) === '/api/admin/v1/posts/post-1/schedule' &&
				(init as RequestInit | undefined)?.method === 'POST'
		);
		expect(scheduleCall).toBeDefined();
	});

	it('schedules successfully with a future date and flips the badge to scheduled', async () => {
		const future = new Date(Date.now() + 2 * 60 * 60 * 1000);
		const pad = (n: number) => String(n).padStart(2, '0');
		const futureValue = `${future.getFullYear()}-${pad(future.getMonth() + 1)}-${pad(future.getDate())}T${pad(future.getHours())}:${pad(future.getMinutes())}`;
		const futureIso = new Date(futureValue).toISOString();

		const fetchMock = routedFetch(
			withReferenceRoutes({
				'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST }),
				'POST /api/admin/v1/posts/post-1/schedule': () =>
					jsonResponse(200, true, {
						post: { ...EXISTING_POST, status: 'scheduled', scheduled_for: futureIso }
					})
			})
		);
		vi.stubGlobal('fetch', fetchMock);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Save' })).toBeInTheDocument();
		});
		await openMenu();
		await fireEvent.click(screen.getByRole('button', { name: 'Schedule…' }));
		await fireEvent.input(screen.getByLabelText('Scheduled for'), {
			target: { value: futureValue }
		});
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

		await waitFor(() => {
			expect(screen.getByText('scheduled')).toBeInTheDocument();
		});
		await openMenu();
		expect(screen.getByRole('button', { name: 'Publish now' })).toBeInTheDocument();
	});
});

describe('PostEditor — preview', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('opens the returned preview_url in a new tab', async () => {
		const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST }),
					'POST /api/admin/v1/posts/post-1/preview-token': () =>
						jsonResponse(200, true, {
							token: 'tok_123',
							expires_at: '2026-08-02T12:15:00Z',
							preview_url: 'https://example.com/api/preview?token=tok_123&slug=existing-post'
						})
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Preview' })).toBeInTheDocument();
		});
		await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));

		await waitFor(() => {
			expect(openSpy).toHaveBeenCalledWith(
				'https://example.com/api/preview?token=tok_123&slug=existing-post',
				'_blank'
			);
		});

		openSpy.mockRestore();
	});

	it('shows a hint pointing to Settings → General when preview_url is null, and does not open a tab', async () => {
		const openSpy = vi.spyOn(window, 'open').mockImplementation(() => null);
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST }),
					'POST /api/admin/v1/posts/post-1/preview-token': () =>
						jsonResponse(200, true, {
							token: 'tok_123',
							expires_at: '2026-08-02T12:15:00Z',
							preview_url: null
						})
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Preview' })).toBeInTheDocument();
		});
		await fireEvent.click(screen.getByRole('button', { name: 'Preview' }));

		await waitFor(() => {
			expect(screen.getByText(/Set a preview URL template/)).toBeInTheDocument();
		});
		expect(openSpy).not.toHaveBeenCalled();

		openSpy.mockRestore();
	});
});

describe('PostEditor — dirty guard on status actions', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('shows a persistent "save first" hint and disables status actions while the form has unsaved edits', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch(
				withReferenceRoutes({
					'GET /api/admin/v1/posts/post-1': () => jsonResponse(200, true, { post: EXISTING_POST })
				})
			)
		);

		render(PostEditor, { props: { postId: 'post-1' } });

		await waitFor(() => {
			expect((screen.getByLabelText('Title') as HTMLInputElement).value).toBe('Existing Post');
		});
		expect(screen.queryByText(/Save your changes first/)).not.toBeInTheDocument();

		await fireEvent.input(screen.getByLabelText('Title'), { target: { value: 'Changed title' } });

		await waitFor(() => {
			expect(screen.getByText(/Save your changes first/)).toBeInTheDocument();
		});
		// The direct Publish button is disabled while dirty…
		expect((screen.getByRole('button', { name: 'Publish' }) as HTMLButtonElement).disabled).toBe(
			true
		);
		// …and so are the status actions in the "..." menu.
		await openMenu();
		expect((screen.getByRole('button', { name: 'Schedule…' }) as HTMLButtonElement).disabled).toBe(
			true
		);
		expect((screen.getByRole('button', { name: 'Archive' }) as HTMLButtonElement).disabled).toBe(
			true
		);
		// Save and Preview stay usable — only status-changing actions are blocked.
		expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(
			false
		);
		expect((screen.getByRole('button', { name: 'Preview' }) as HTMLButtonElement).disabled).toBe(
			false
		);
	});
});

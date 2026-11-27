import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { auth } from '$lib/auth.svelte';
import Page from './+page.svelte';

const OWNER = {
	id: 'owner-1',
	name: 'Ada Lovelace',
	email: 'ada@example.com',
	role: 'owner' as const,
	active: true,
	last_login_at: null,
	created_at: '2024-01-01T00:00:00Z'
};

function jsonResponse(status: number, ok: boolean, body: unknown) {
	return { ok, status, text: async () => JSON.stringify(body) };
}

const POST_A = {
	id: 'post-a',
	title: 'Announcing PocketPress',
	slug: 'announcing-pocketpress',
	status: 'draft' as const,
	excerpt: 'A calm little blog backend.',
	featured: false,
	author: { id: 'owner-1', name: 'Ada Lovelace' },
	categories: [],
	tags: [],
	scheduled_for: null,
	published_at: null,
	created_at: '2024-01-01T00:00:00Z',
	updated_at: '2024-01-05T00:00:00Z'
};

const POST_B = {
	...POST_A,
	id: 'post-b',
	title: 'A Second Post',
	status: 'archived' as const,
	excerpt: 'Older, archived.'
};

function defaultPostsResponse() {
	return {
		posts: [POST_A, POST_B],
		meta: { page: 1, per_page: 20, total_items: 2, total_pages: 1 }
	};
}

/** Routes every fetch call by method + path prefix (query strings vary and
 * are inspected separately via fetchMock.mock.calls). */
function routedFetch(overrides: {
	posts?: () => ReturnType<typeof jsonResponse>;
	duplicate?: () => ReturnType<typeof jsonResponse>;
	delete?: () => ReturnType<typeof jsonResponse>;
	categories?: () => ReturnType<typeof jsonResponse>;
	importMarkdown?: () => ReturnType<typeof jsonResponse>;
	importGhost?: () => ReturnType<typeof jsonResponse>;
	importWordPress?: () => ReturnType<typeof jsonResponse>;
}) {
	return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === 'string' ? input : input.toString();
		const method = (init?.method ?? 'GET').toUpperCase();

		if (url.startsWith('/api/admin/v1/categories')) {
			return (overrides.categories ?? (() => jsonResponse(200, true, { categories: [] })))();
		}
		if (method === 'POST' && url.includes('/import/markdown')) {
			return (
				overrides.importMarkdown ??
				(() => jsonResponse(200, true, { imported: [], failed: [] }))
			)();
		}
		if (method === 'POST' && url.includes('/import/ghost')) {
			return (
				overrides.importGhost ??
				(() => jsonResponse(200, true, { imported: [], failed: [] }))
			)();
		}
		if (method === 'POST' && url.includes('/import/wordpress')) {
			return (
				overrides.importWordPress ??
				(() => jsonResponse(200, true, { imported: [], failed: [] }))
			)();
		}
		if (method === 'POST' && url.includes('/duplicate')) {
			return (overrides.duplicate ?? (() => jsonResponse(201, true, { post: POST_A })))();
		}
		if (method === 'DELETE') {
			return (overrides.delete ?? (() => jsonResponse(204, true, null)))();
		}
		if (url.startsWith('/api/admin/v1/posts')) {
			return (overrides.posts ?? (() => jsonResponse(200, true, defaultPostsResponse())))();
		}
		throw new Error(`Unhandled request in test: ${method} ${url}`);
	});
}

describe('Posts list', () => {
	beforeEach(() => {
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('renders post rows with title, status, author and updated date', async () => {
		vi.stubGlobal('fetch', routedFetch({}));

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});
		expect(screen.getByText('A Second Post')).toBeInTheDocument();
		expect(screen.getByText('draft')).toBeInTheDocument();
		expect(screen.getByText('archived')).toBeInTheDocument();
		// Both post rows list the same author. The sidebar (which also shows the
		// signed-in user's name) is rendered by the layout, not this page.
		expect(screen.getAllByText('Ada Lovelace')).toHaveLength(2);
	});

	it('links the New post button to /posts/new', async () => {
		vi.stubGlobal('fetch', routedFetch({}));

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});
		expect(screen.getByRole('link', { name: 'New post' })).toHaveAttribute('href', '/posts/new');
	});

	it('imports Markdown files and refreshes the list', async () => {
		const fetchMock = routedFetch({
			importMarkdown: () =>
				jsonResponse(200, true, {
					imported: [{ filename: 'hello.md', post: { ...POST_A, id: 'post-imported', title: 'Imported' } }],
					failed: []
				})
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		const fileInput = document.querySelector(
			'input[type="file"][accept*=".md"]'
		) as HTMLInputElement;
		expect(fileInput).toBeTruthy();

		const file = new File(['# Imported\n\nHi.\n'], 'hello.md', { type: 'text/markdown' });
		await fireEvent.change(fileInput, { target: { files: [file] } });

		await waitFor(() => {
			expect(screen.getByText('Imported 1 post from Markdown.')).toBeInTheDocument();
		});
		const calledImport = fetchMock.mock.calls.some(
			([reqUrl, init]) =>
				String(reqUrl).includes('/posts/import/markdown') &&
				(init as RequestInit | undefined)?.method === 'POST'
		);
		expect(calledImport).toBe(true);
	});

	it('imports a Ghost export and refreshes the list', async () => {
		const fetchMock = routedFetch({
			importGhost: () =>
				jsonResponse(200, true, {
					imported: [
						{
							source_key: 'hello-ghost',
							post: { ...POST_A, id: 'post-ghost', title: 'Ghost Post' }
						}
					],
					failed: []
				})
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		const fileInput = document.querySelector(
			'input[type="file"][accept*=".json"]'
		) as HTMLInputElement;
		const file = new File(['{"posts":[]}'], 'ghost.json', { type: 'application/json' });
		await fireEvent.change(fileInput, { target: { files: [file] } });

		await waitFor(() => {
			expect(screen.getByText('Imported 1 post from Ghost.')).toBeInTheDocument();
		});
		expect(
			fetchMock.mock.calls.some(
				([reqUrl, init]) =>
					String(reqUrl).includes('/posts/import/ghost') &&
					(init as RequestInit | undefined)?.method === 'POST'
			)
		).toBe(true);
	});

	it('imports a WordPress export and refreshes the list', async () => {
		const fetchMock = routedFetch({
			importWordPress: () =>
				jsonResponse(200, true, {
					imported: [
						{
							source_key: 'hello-wp',
							post: { ...POST_A, id: 'post-wp', title: 'WP Post' }
						}
					],
					failed: []
				})
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		const fileInput = document.querySelector(
			'input[type="file"][accept*=".xml"]'
		) as HTMLInputElement;
		const file = new File(['<rss></rss>'], 'wordpress.xml', { type: 'text/xml' });
		await fireEvent.change(fileInput, { target: { files: [file] } });

		await waitFor(() => {
			expect(screen.getByText('Imported 1 post from WordPress.')).toBeInTheDocument();
		});
		expect(
			fetchMock.mock.calls.some(
				([reqUrl, init]) =>
					String(reqUrl).includes('/posts/import/wordpress') &&
					(init as RequestInit | undefined)?.method === 'POST'
			)
		).toBe(true);
	});

	it('requests status=draft when the status filter changes', async () => {
		const fetchMock = routedFetch({});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		await fireEvent.change(screen.getByLabelText('Status'), { target: { value: 'draft' } });

		await waitFor(() => {
			const calledWithStatus = fetchMock.mock.calls.some(([reqUrl]) =>
				String(reqUrl).includes('status=draft')
			);
			expect(calledWithStatus).toBe(true);
		});
	});

	it('requests category=<id> when the category filter changes', async () => {
		const fetchMock = routedFetch({
			categories: () =>
				jsonResponse(200, true, {
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
				})
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		await fireEvent.change(screen.getByLabelText('Category'), { target: { value: 'cat-1' } });

		await waitFor(() => {
			const calledWithCategory = fetchMock.mock.calls.some(([reqUrl]) =>
				String(reqUrl).includes('category=cat-1')
			);
			expect(calledWithCategory).toBe(true);
		});
	});

	it('debounces the search input and requests search=<query>', async () => {
		const fetchMock = routedFetch({});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		await fireEvent.input(screen.getByLabelText('Search'), { target: { value: 'pocketpress' } });

		await waitFor(
			() => {
				const calledWithSearch = fetchMock.mock.calls.some(([reqUrl]) =>
					String(reqUrl).includes('search=pocketpress')
				);
				expect(calledWithSearch).toBe(true);
			},
			{ timeout: 2000 }
		);
	});

	it('duplicates a post and refreshes the list', async () => {
		const fetchMock = routedFetch({});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		const duplicateButtons = screen.getAllByRole('button', { name: 'Duplicate' });
		await fireEvent.click(duplicateButtons[0]);

		await waitFor(() => {
			const calledDuplicate = fetchMock.mock.calls.some(
				([reqUrl, init]) =>
					String(reqUrl).includes('/duplicate') &&
					(init as RequestInit | undefined)?.method === 'POST'
			);
			expect(calledDuplicate).toBe(true);
		});
	});

	it('deletes a post after inline confirmation', async () => {
		const fetchMock = routedFetch({});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
		await fireEvent.click(deleteButtons[0]);

		await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

		await waitFor(() => {
			const calledDelete = fetchMock.mock.calls.some(
				([, init]) => (init as RequestInit | undefined)?.method === 'DELETE'
			);
			expect(calledDelete).toBe(true);
		});
	});

	it('offers all four status options in the filter, including scheduled and published', async () => {
		vi.stubGlobal('fetch', routedFetch({}));

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Announcing PocketPress')).toBeInTheDocument();
		});

		const statusSelect = screen.getByLabelText('Status') as HTMLSelectElement;
		const optionValues = Array.from(statusSelect.options).map((option) => option.value);
		expect(optionValues).toEqual(['all', 'draft', 'scheduled', 'published', 'archived']);
	});

	it('shows distinct status badges for scheduled/published, and a scheduled date only for scheduled posts', async () => {
		const POST_SCHEDULED = {
			...POST_A,
			id: 'post-c',
			title: 'Scheduled Post',
			status: 'scheduled' as const,
			scheduled_for: '2026-09-01T10:00:00Z'
		};
		const POST_PUBLISHED = {
			...POST_A,
			id: 'post-d',
			title: 'Published Post',
			status: 'published' as const,
			published_at: '2026-07-01T09:00:00Z'
		};

		vi.stubGlobal(
			'fetch',
			routedFetch({
				posts: () =>
					jsonResponse(200, true, {
						posts: [POST_SCHEDULED, POST_PUBLISHED],
						meta: { page: 1, per_page: 20, total_items: 2, total_pages: 1 }
					})
			})
		);

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Scheduled Post')).toBeInTheDocument();
		});
		expect(screen.getByText('scheduled')).toBeInTheDocument();
		expect(screen.getByText('published')).toBeInTheDocument();

		const scheduledRow = screen.getByText('Scheduled Post').closest('tr');
		expect(scheduledRow?.textContent).toMatch(/2026/);

		const publishedRow = screen.getByText('Published Post').closest('tr');
		expect(publishedRow?.textContent).toContain('—');
	});

	it('shows an empty state when no posts match the filters', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch({
				posts: () =>
					jsonResponse(200, true, {
						posts: [],
						meta: { page: 1, per_page: 20, total_items: 0, total_pages: 1 }
					})
			})
		);

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('No posts match these filters yet.')).toBeInTheDocument();
		});
	});
});

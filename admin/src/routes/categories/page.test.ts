import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { auth } from '$lib/auth.svelte';
import Page from './+page.svelte';

const OWNER = {
	id: 'owner-1',
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

const NEWS_CATEGORY = {
	id: 'cat-1',
	name: 'News',
	slug: 'news',
	description: 'Announcements',
	created_at: '2024-01-01T00:00:00Z',
	updated_at: '2024-01-01T00:00:00Z'
};

describe('Categories page', () => {
	beforeEach(() => {
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('renders existing categories with name, slug and description', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn().mockResolvedValue(jsonResponse(200, true, { categories: [NEWS_CATEGORY] }))
		);

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('News')).toBeInTheDocument();
		});
		expect(screen.getByText('/news')).toBeInTheDocument();
		expect(screen.getByText('Announcements')).toBeInTheDocument();
	});

	it('creates a category and appends it to the list', async () => {
		const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = typeof input === 'string' ? input : input.toString();
			const method = (init?.method ?? 'GET').toUpperCase();
			if (method === 'POST') {
				const body = JSON.parse(String(init?.body));
				expect(body).toEqual({ name: 'Guides', slug: undefined, description: undefined });
				return jsonResponse(201, true, {
					category: {
						id: 'cat-2',
						name: 'Guides',
						slug: 'guides',
						description: null,
						created_at: '2024-01-02T00:00:00Z',
						updated_at: '2024-01-02T00:00:00Z'
					}
				});
			}
			if (url.includes('/categories')) {
				return jsonResponse(200, true, { categories: [] });
			}
			throw new Error(`Unhandled request: ${method} ${url}`);
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('No categories yet — add your first one below.')).toBeInTheDocument();
		});

		await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Guides' } });
		await fireEvent.click(screen.getByRole('button', { name: 'New category' }));

		await waitFor(() => {
			expect(screen.getByText('Guides')).toBeInTheDocument();
		});
	});

	it('renders validation_failed messages next to the create form fields', async () => {
		vi.stubGlobal('fetch', async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = typeof input === 'string' ? input : input.toString();
			const method = (init?.method ?? 'GET').toUpperCase();
			if (method === 'POST') {
				return jsonResponse(400, false, {
					error: {
						code: 'validation_failed',
						message: 'Validation failed',
						details: { name: 'Name is required' }
					}
				});
			}
			if (url.includes('/categories')) {
				return jsonResponse(200, true, { categories: [] });
			}
			throw new Error(`Unhandled request: ${method} ${url}`);
		});

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('No categories yet — add your first one below.')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'New category' }));

		await waitFor(() => {
			expect(screen.getByText('Name is required')).toBeInTheDocument();
		});
	});

	it('deletes a category after inline confirmation', async () => {
		const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = typeof input === 'string' ? input : input.toString();
			const method = (init?.method ?? 'GET').toUpperCase();
			if (method === 'DELETE') {
				return jsonResponse(204, true, null);
			}
			if (url.includes('/categories')) {
				return jsonResponse(200, true, { categories: [NEWS_CATEGORY] });
			}
			throw new Error(`Unhandled request: ${method} ${url}`);
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('News')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

		await waitFor(() => {
			expect(screen.queryByText('News')).not.toBeInTheDocument();
		});
		expect(
			fetchMock.mock.calls.some(
				([, init]) => (init as RequestInit | undefined)?.method === 'DELETE'
			)
		).toBe(true);
	});
});

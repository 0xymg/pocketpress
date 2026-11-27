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

const CHANGELOG = {
	id: 'tmpl-1',
	name: 'Changelog',
	description: 'Monthly notes',
	title: 'Changelog: ',
	excerpt: '',
	content_json: '{}',
	content_html: '',
	cover_image: null,
	categories: [],
	tags: [],
	featured: false,
	seo_title: '',
	seo_description: '',
	canonical_url: '',
	og_image: null,
	locale: 'en',
	created_by: 'owner-1',
	created_at: '2024-01-01T00:00:00Z',
	updated_at: '2024-01-01T00:00:00Z'
};

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

describe('Templates page', () => {
	beforeEach(() => {
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('renders existing templates', async () => {
		vi.stubGlobal(
			'fetch',
			vi.fn().mockResolvedValue(jsonResponse(200, true, { templates: [CHANGELOG] }))
		);

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Changelog')).toBeInTheDocument();
		});
		expect(screen.getByText('Monthly notes')).toBeInTheDocument();
		expect(screen.getByText(/Starts as “Changelog: ”/)).toBeInTheDocument();
	});

	it('creates a template and appends it to the list', async () => {
		const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = typeof input === 'string' ? input : input.toString();
			const method = (init?.method ?? 'GET').toUpperCase();
			if (method === 'POST' && url.includes('/templates') && !url.includes('create-post')) {
				const body = JSON.parse(String(init?.body));
				expect(body.name).toBe('Release notes');
				return jsonResponse(201, true, {
					template: {
						...CHANGELOG,
						id: 'tmpl-2',
						name: 'Release notes',
						description: '',
						title: 'Release: '
					}
				});
			}
			if (url.includes('/templates')) {
				return jsonResponse(200, true, { templates: [] });
			}
			throw new Error(`Unhandled request: ${method} ${url}`);
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('No templates yet — add your first one below.')).toBeInTheDocument();
		});

		await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Release notes' } });
		await fireEvent.input(screen.getByLabelText('Starting title'), {
			target: { value: 'Release: ' }
		});
		await fireEvent.click(screen.getByRole('button', { name: 'New template' }));

		await waitFor(() => {
			expect(screen.getByText('Release notes')).toBeInTheDocument();
		});
	});

	it('uses a template and navigates to the new draft', async () => {
		const { goto } = await import('$app/navigation');
		const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = typeof input === 'string' ? input : input.toString();
			const method = (init?.method ?? 'GET').toUpperCase();
			if (method === 'POST' && url.includes('/create-post')) {
				return jsonResponse(201, true, {
					post: { id: 'post-99', title: 'Changelog: ', status: 'draft' }
				});
			}
			if (url.includes('/templates')) {
				return jsonResponse(200, true, { templates: [CHANGELOG] });
			}
			throw new Error(`Unhandled request: ${method} ${url}`);
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Changelog')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Use template' }));

		await waitFor(() => {
			expect(goto).toHaveBeenCalledWith('/posts/post-99');
		});
	});
});

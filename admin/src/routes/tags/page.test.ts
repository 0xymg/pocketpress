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

const LAUNCH_TAG = {
	id: 'tag-1',
	name: 'Launch',
	slug: 'launch',
	created_at: '2024-01-01T00:00:00Z',
	updated_at: '2024-01-01T00:00:00Z'
};

describe('Tags page', () => {
	beforeEach(() => {
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('renders existing tags with name and slug', async () => {
		vi.stubGlobal('fetch', vi.fn().mockResolvedValue(jsonResponse(200, true, { tags: [LAUNCH_TAG] })));

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Launch')).toBeInTheDocument();
		});
		expect(screen.getByText('/launch')).toBeInTheDocument();
	});

	it('creates a tag and appends it to the list', async () => {
		const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = typeof input === 'string' ? input : input.toString();
			const method = (init?.method ?? 'GET').toUpperCase();
			if (method === 'POST') {
				const body = JSON.parse(String(init?.body));
				expect(body).toEqual({ name: 'Release', slug: undefined });
				return jsonResponse(201, true, {
					tag: {
						id: 'tag-2',
						name: 'Release',
						slug: 'release',
						created_at: '2024-01-02T00:00:00Z',
						updated_at: '2024-01-02T00:00:00Z'
					}
				});
			}
			if (url.includes('/tags')) {
				return jsonResponse(200, true, { tags: [] });
			}
			throw new Error(`Unhandled request: ${method} ${url}`);
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('No tags yet — add your first one below.')).toBeInTheDocument();
		});

		await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Release' } });
		await fireEvent.click(screen.getByRole('button', { name: 'New tag' }));

		await waitFor(() => {
			expect(screen.getByText('Release')).toBeInTheDocument();
		});
	});

	it('deletes a tag after inline confirmation', async () => {
		const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
			const url = typeof input === 'string' ? input : input.toString();
			const method = (init?.method ?? 'GET').toUpperCase();
			if (method === 'DELETE') {
				return jsonResponse(204, true, null);
			}
			if (url.includes('/tags')) {
				return jsonResponse(200, true, { tags: [LAUNCH_TAG] });
			}
			throw new Error(`Unhandled request: ${method} ${url}`);
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Launch')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));

		await waitFor(() => {
			expect(screen.queryByText('Launch')).not.toBeInTheDocument();
		});
		expect(
			fetchMock.mock.calls.some(
				([, init]) => (init as RequestInit | undefined)?.method === 'DELETE'
			)
		).toBe(true);
	});
});

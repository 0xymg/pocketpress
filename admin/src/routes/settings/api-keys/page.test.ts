import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

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

const EDITOR = {
	id: 'editor-1',
	name: 'Grace Hopper',
	email: 'grace@example.com',
	role: 'editor' as const,
	slug: 'ada-lovelace',
	bio: '',
	website: '',
	location: '',
	avatar: '',
	active: true,
	last_login_at: null,
	created_at: '2024-01-01T00:00:00Z'
};

const KEY_A = {
	id: 'ak-1',
	name: 'CI reader',
	prefix: 'ppkey_abc123',
	scopes: ['content:read'],
	expires_at: null,
	last_used_at: null,
	revoked_at: null,
	created_at: '2026-08-01T00:00:00Z'
};

function jsonResponse(status: number, ok: boolean, body: unknown) {
	return { ok, status, text: async () => JSON.stringify(body) };
}

function routedFetch(overrides: {
	list?: () => ReturnType<typeof jsonResponse>;
	create?: () => ReturnType<typeof jsonResponse>;
	revoke?: () => ReturnType<typeof jsonResponse>;
	del?: () => ReturnType<typeof jsonResponse>;
}) {
	return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === 'string' ? input : input.toString();
		const method = (init?.method ?? 'GET').toUpperCase();
		if (method === 'POST' && url.includes('/revoke')) {
			return (
				overrides.revoke ??
				(() =>
					jsonResponse(200, true, {
						api_key: { ...KEY_A, revoked_at: '2026-08-02T00:00:00Z' }
					}))
			)();
		}
		if (method === 'DELETE') {
			return (overrides.del ?? (() => jsonResponse(204, true, null)))();
		}
		if (method === 'POST' && url.endsWith('/api-keys')) {
			return (
				overrides.create ??
				(() =>
					jsonResponse(201, true, {
						api_key: KEY_A,
						key: 'ppkey_abc123_secret_only_once'
					}))
			)();
		}
		if (method === 'GET' && url.includes('/api-keys')) {
			return (overrides.list ?? (() => jsonResponse(200, true, { api_keys: [KEY_A] })))();
		}
		return jsonResponse(404, false, { error: { code: 'not_found', message: 'missing', details: null } });
	});
}

describe('/settings/api-keys', () => {
	afterEach(() => {
		auth.user = null;
		vi.unstubAllGlobals();
		vi.restoreAllMocks();
	});

	it('blocks editors with a friendly no-access state and never calls the API', async () => {
		auth.user = EDITOR;
		const fetchMock = routedFetch({});
		vi.stubGlobal('fetch', fetchMock);
		render(Page);
		expect(screen.getByText('Only the site owner can manage API keys.')).toBeInTheDocument();
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it('lists keys for owners and shows the raw key only after create', async () => {
		auth.user = OWNER;
		const fetchMock = routedFetch({
			list: () => jsonResponse(200, true, { api_keys: [] })
		});
		vi.stubGlobal('fetch', fetchMock);
		render(Page);

		await waitFor(() => {
			expect(screen.getByText('No API keys yet — create one above.')).toBeInTheDocument();
		});

		await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'CI reader' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Create key' }));

		await waitFor(() => {
			expect(screen.getByText('Copy your API key')).toBeInTheDocument();
			expect(screen.getByText('ppkey_abc123_secret_only_once')).toBeInTheDocument();
			expect(screen.getByText('CI reader')).toBeInTheDocument();
		});
	});

	it('loads existing keys', async () => {
		auth.user = OWNER;
		vi.stubGlobal('fetch', routedFetch({}));
		render(Page);
		await waitFor(() => {
			expect(screen.getByText('CI reader')).toBeInTheDocument();
			expect(screen.getByText(/ppkey_abc123/)).toBeInTheDocument();
		});
	});
});

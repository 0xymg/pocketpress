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

const SETTINGS = {
	site_name: 'PocketPress Blog',
	site_url: 'https://example.com',
	admin_title: 'PocketPress Admin',
	locale: 'en',
	timezone: 'UTC',
	default_author: null,
	cors_origins: ['https://example.com', 'https://www.example.com'],
	public_api_enabled: true,
	preview_url_template: 'https://example.com/api/preview?token={token}&slug={slug}',
	webhook_user_agent: 'PocketPress/0.1.0'
};

function jsonResponse(status: number, ok: boolean, body: unknown) {
	return { ok, status, text: async () => JSON.stringify(body) };
}

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

describe('General settings page', () => {
	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('loads and renders settings for an owner', async () => {
		auth.user = OWNER;
		vi.stubGlobal(
			'fetch',
			routedFetch({
				'GET /api/admin/v1/settings': () => jsonResponse(200, true, { settings: SETTINGS })
			})
		);

		render(Page);

		await waitFor(() => {
			expect((screen.getByLabelText('Site name') as HTMLInputElement).value).toBe(
				'PocketPress Blog'
			);
		});
		expect((screen.getByLabelText('Site URL') as HTMLInputElement).value).toBe(
			'https://example.com'
		);
		expect((screen.getByLabelText('Locale') as HTMLInputElement).value).toBe('en');
		expect((screen.getByLabelText(/CORS origins/) as HTMLTextAreaElement).value).toBe(
			'https://example.com\nhttps://www.example.com'
		);
		expect((screen.getByLabelText('Public API enabled') as HTMLInputElement).checked).toBe(true);
		// The exact placeholder/example string documented for {token}/{slug}.
		expect(
			screen.getByText('https://example.com/api/preview?token={token}&slug={slug}')
		).toBeInTheDocument();
	});

	it('blocks editors with a friendly no-access state and never calls the settings API', async () => {
		auth.user = EDITOR;
		const fetchMock = routedFetch({
			'GET /api/admin/v1/settings': () => jsonResponse(200, true, { settings: SETTINGS })
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);

		expect(
			screen.getByText("You don't have access to this page. Ask an owner for access.")
		).toBeInTheDocument();
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it('PATCHes only the fields that changed, leaving the rest untouched', async () => {
		auth.user = OWNER;
		const fetchMock = routedFetch({
			'GET /api/admin/v1/settings': () => jsonResponse(200, true, { settings: SETTINGS }),
			'PATCH /api/admin/v1/settings': () =>
				jsonResponse(200, true, { settings: { ...SETTINGS, site_name: 'New Name' } })
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);

		await waitFor(() => {
			expect((screen.getByLabelText('Site name') as HTMLInputElement).value).toBe(
				'PocketPress Blog'
			);
		});

		await fireEvent.input(screen.getByLabelText('Site name'), { target: { value: 'New Name' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

		await waitFor(() => {
			expect(screen.getByText(/^Saved \d{2}:\d{2}$/)).toBeInTheDocument();
		});

		const patchCall = fetchMock.mock.calls.find(
			([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
		);
		expect(patchCall).toBeDefined();
		const body = JSON.parse(String((patchCall?.[1] as RequestInit).body));
		expect(body).toEqual({ site_name: 'New Name' });
	});

	it('sends cors_origins as a diffed array, one origin per textarea line', async () => {
		auth.user = OWNER;
		const fetchMock = routedFetch({
			'GET /api/admin/v1/settings': () => jsonResponse(200, true, { settings: SETTINGS }),
			'PATCH /api/admin/v1/settings': () =>
				jsonResponse(200, true, {
					settings: { ...SETTINGS, cors_origins: ['https://example.com'] }
				})
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);

		await waitFor(() => {
			expect((screen.getByLabelText(/CORS origins/) as HTMLTextAreaElement).value).toContain(
				'https://www.example.com'
			);
		});

		await fireEvent.input(screen.getByLabelText(/CORS origins/), {
			target: { value: 'https://example.com' }
		});
		await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

		await waitFor(() => {
			expect(screen.getByText(/^Saved \d{2}:\d{2}$/)).toBeInTheDocument();
		});

		const patchCall = fetchMock.mock.calls.find(
			([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
		);
		const body = JSON.parse(String((patchCall?.[1] as RequestInit).body));
		expect(body).toEqual({ cors_origins: ['https://example.com'] });
	});

	it('renders per-field validation_failed errors from the server', async () => {
		auth.user = OWNER;
		vi.stubGlobal(
			'fetch',
			routedFetch({
				'GET /api/admin/v1/settings': () => jsonResponse(200, true, { settings: SETTINGS }),
				'PATCH /api/admin/v1/settings': () =>
					jsonResponse(400, false, {
						error: {
							code: 'validation_failed',
							message: 'Validation failed',
							details: { site_url: 'Site URL must be a valid URL.' }
						}
					})
			})
		);

		render(Page);

		await waitFor(() => {
			expect((screen.getByLabelText('Site URL') as HTMLInputElement).value).toBe(
				'https://example.com'
			);
		});

		await fireEvent.input(screen.getByLabelText('Site URL'), { target: { value: 'not-a-url' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

		await waitFor(() => {
			expect(screen.getByText('Site URL must be a valid URL.')).toBeInTheDocument();
		});
	});

	it('does not call PATCH when nothing changed', async () => {
		auth.user = OWNER;
		const fetchMock = routedFetch({
			'GET /api/admin/v1/settings': () => jsonResponse(200, true, { settings: SETTINGS })
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);

		await waitFor(() => {
			expect((screen.getByLabelText('Site name') as HTMLInputElement).value).toBe(
				'PocketPress Blog'
			);
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Save' }));

		await waitFor(() => {
			expect(screen.getByText(/^Saved \d{2}:\d{2}$/)).toBeInTheDocument();
		});
		expect(
			fetchMock.mock.calls.some(([, init]) => (init as RequestInit | undefined)?.method === 'PATCH')
		).toBe(false);
	});
});

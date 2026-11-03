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

const SUNSET_PHOTO = {
	id: 'media-1',
	url: '/api/files/media-1/sunset.jpg',
	filename: 'sunset.jpg',
	mime: 'image/jpeg',
	size: 245_000,
	width: 1200,
	height: 800,
	alt: 'A sunset over the bay',
	created_at: '2024-01-01T00:00:00Z'
};

function defaultMediaResponse() {
	return {
		media: [SUNSET_PHOTO],
		meta: { page: 1, per_page: 40, total_items: 1, total_pages: 1 }
	};
}

/** Routes fetch calls by "METHOD path-prefix"; query strings are inspected via fetchMock.mock.calls. */
function routedFetch(overrides: {
	list?: (url: string) => ReturnType<typeof jsonResponse>;
	upload?: () => ReturnType<typeof jsonResponse>;
	patch?: () => ReturnType<typeof jsonResponse>;
	del?: () => ReturnType<typeof jsonResponse>;
}) {
	return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === 'string' ? input : input.toString();
		const method = (init?.method ?? 'GET').toUpperCase();
		if (method === 'GET' && url.includes('/api/admin/v1/media')) {
			const list = overrides.list ?? (() => jsonResponse(200, true, defaultMediaResponse()));
			return list(url);
		}
		if (method === 'POST' && url.includes('/api/admin/v1/media')) {
			return (overrides.upload ?? (() => jsonResponse(201, true, { media: SUNSET_PHOTO })))();
		}
		if (method === 'PATCH' && url.includes('/api/admin/v1/media/')) {
			return (overrides.patch ?? (() => jsonResponse(200, true, { media: SUNSET_PHOTO })))();
		}
		if (method === 'DELETE' && url.includes('/api/admin/v1/media/')) {
			return (overrides.del ?? (() => jsonResponse(204, true, null)))();
		}
		throw new Error(`Unhandled request in test: ${method} ${url}`);
	});
}

describe('Media page', () => {
	beforeEach(() => {
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('renders the grid from the mocked list, including name, dimensions and size', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch({ list: () => jsonResponse(200, true, defaultMediaResponse()) })
		);

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('sunset.jpg')).toBeInTheDocument();
		});
		expect(screen.getByText(/1200×800/)).toBeInTheDocument();
		expect(screen.getByText(/239 KB/)).toBeInTheDocument();
	});

	it('debounces the search input and requests search=<query>', async () => {
		const fetchMock = routedFetch({ list: () => jsonResponse(200, true, defaultMediaResponse()) });
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('sunset.jpg')).toBeInTheDocument();
		});

		await fireEvent.input(screen.getByLabelText('Search'), { target: { value: 'sunset' } });

		await waitFor(
			() => {
				const calledWithSearch = fetchMock.mock.calls.some(([reqUrl]) =>
					String(reqUrl).includes('search=sunset')
				);
				expect(calledWithSearch).toBe(true);
			},
			{ timeout: 2000 }
		);
	});

	it('uploads a selected file as multipart form data and refreshes the grid', async () => {
		let listCalls = 0;
		const fetchMock = routedFetch({
			list: () => {
				listCalls += 1;
				return jsonResponse(
					200,
					true,
					listCalls === 1
						? { media: [], meta: { page: 1, per_page: 40, total_items: 0, total_pages: 1 } }
						: defaultMediaResponse()
				);
			},
			upload: () => jsonResponse(201, true, { media: SUNSET_PHOTO })
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('No media yet — upload your first image.')).toBeInTheDocument();
		});

		const file = new File(['fake-bytes'], 'sunset.jpg', { type: 'image/jpeg' });
		const input = document.querySelector('input[type="file"]') as HTMLInputElement;
		await fireEvent.change(input, { target: { files: [file] } });

		await waitFor(() => {
			expect(screen.getByText('sunset.jpg')).toBeInTheDocument();
		});

		const uploadCall = fetchMock.mock.calls.find(
			([, init]) => (init as RequestInit | undefined)?.method === 'POST'
		);
		expect(uploadCall).toBeDefined();
		const uploadInit = uploadCall?.[1] as RequestInit;
		expect(uploadInit.body).toBeInstanceOf(FormData);
		// No manual Content-Type — the browser must set the multipart boundary itself.
		expect((uploadInit.headers as Record<string, string> | undefined)?.['Content-Type']).toBeUndefined();
	});

	it('shows an inline message when delete fails because the image is in use', async () => {
		vi.stubGlobal(
			'fetch',
			routedFetch({
				list: () => jsonResponse(200, true, defaultMediaResponse()),
				del: () =>
					jsonResponse(400, false, {
						error: { code: 'media_in_use', message: 'Media is in use' }
					})
			})
		);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('sunset.jpg')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByText('sunset.jpg'));

		await waitFor(() => {
			expect(screen.getByRole('button', { name: 'Delete' })).toBeInTheDocument();
		});
		await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm delete' }));

		await waitFor(() => {
			expect(screen.getByText('Used as a cover/OG image on a post.')).toBeInTheDocument();
		});
	});
});

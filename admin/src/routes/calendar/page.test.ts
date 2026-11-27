import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { auth } from '$lib/auth.svelte';
import Page from './+page.svelte';

const OWNER = {
	id: '1',
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

function entry(overrides: Record<string, unknown> = {}) {
	return {
		id: 'post-1',
		title: 'Launch notes',
		slug: 'launch-notes',
		status: 'scheduled',
		locale: 'en',
		author: { id: '1', name: 'Ada Lovelace', slug: 'ada-lovelace' },
		at: '2026-11-15T10:00:00Z',
		scheduled_for: '2026-11-15T10:00:00Z',
		published_at: null,
		...overrides
	};
}

function calendarFetch(entries: ReturnType<typeof entry>[] = [entry()]) {
	return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === 'string' ? input : input.toString();
		if (url.includes('/calendar')) {
			return {
				ok: true,
				status: 200,
				text: async () => JSON.stringify({ entries })
			};
		}
		if (url.includes('/schedule') && init?.method === 'POST') {
			const body = JSON.parse(String(init.body));
			return {
				ok: true,
				status: 200,
				text: async () =>
					JSON.stringify({
						post: {
							...entry(),
							status: 'scheduled',
							scheduled_for: body.scheduled_for,
							content_json: '{}',
							content_html: '',
							seo_title: '',
							seo_description: '',
							canonical_url: '',
							translation_group: null,
							translations: [],
							categories: [],
							tags: [],
							cover_image: null,
							og_image: null,
							excerpt: '',
							featured: false,
							created_at: '2026-11-01T00:00:00Z',
							updated_at: '2026-11-01T00:00:00Z'
						}
					})
			};
		}
		throw new Error(`Unhandled request in test: ${url}`);
	});
}

describe('Calendar page', () => {
	beforeEach(() => {
		auth.user = OWNER;
		vi.useFakeTimers({ shouldAdvanceTime: true });
		vi.setSystemTime(new Date('2026-11-15T12:00:00Z'));
	});

	afterEach(() => {
		vi.useRealTimers();
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('loads entries for the visible month and shows them on the grid', async () => {
		const fetchMock = calendarFetch([
			entry(),
			entry({
				id: 'post-2',
				title: 'Shipped feature',
				status: 'published',
				at: '2026-11-10T09:00:00Z',
				scheduled_for: null,
				published_at: '2026-11-10T09:00:00Z'
			})
		]);
		vi.stubGlobal('fetch', fetchMock);

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Launch notes')).toBeInTheDocument();
			expect(screen.getByText('Shipped feature')).toBeInTheDocument();
		});

		expect(fetchMock).toHaveBeenCalled();
		const calendarCall = fetchMock.mock.calls.find((c) =>
			String(c[0]).includes('/api/admin/v1/calendar')
		);
		expect(calendarCall).toBeTruthy();
		expect(String(calendarCall![0])).toContain('from=');
		expect(String(calendarCall![0])).toContain('to=');
	});

	it('opens a detail panel with reschedule for scheduled posts', async () => {
		vi.stubGlobal('fetch', calendarFetch());

		render(Page);

		await waitFor(() => expect(screen.getByText('Launch notes')).toBeInTheDocument());
		await fireEvent.click(screen.getByText('Launch notes'));

		expect(screen.getByLabelText('Selected post')).toBeInTheDocument();
		expect(screen.getByRole('link', { name: 'Open editor' })).toHaveAttribute(
			'href',
			'/posts/post-1'
		);
		expect(screen.getByLabelText('Reschedule')).toBeInTheDocument();
	});

	it('navigates months with Previous/Next', async () => {
		const fetchMock = calendarFetch([]);
		vi.stubGlobal('fetch', fetchMock);

		render(Page);

		await waitFor(() => expect(screen.getByText(/November/i)).toBeInTheDocument());
		await fireEvent.click(screen.getByRole('button', { name: 'Next month' }));
		await waitFor(() => expect(screen.getByText(/December/i)).toBeInTheDocument());
		await fireEvent.click(screen.getByRole('button', { name: 'Previous month' }));
		await waitFor(() => expect(screen.getByText(/November/i)).toBeInTheDocument());
	});
});

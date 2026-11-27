import { render, screen, waitFor } from '@testing-library/svelte';
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

function summary(overrides: Record<string, unknown> = {}) {
	return {
		id: 'post-1',
		title: 'A recent post',
		slug: 'a-recent-post',
		status: 'draft',
		excerpt: '',
		featured: false,
		author: { id: '1', name: 'Ada Lovelace', slug: 'ada-lovelace' },
		categories: [],
		tags: [],
		scheduled_for: null,
		published_at: null,
		created_at: '2024-01-01T00:00:00Z',
		updated_at: '2024-02-01T00:00:00Z',
		...overrides
	};
}

/**
 * One stub for both the health check (reads response.json) and the admin API
 * (reads response.text). The dashboard fires /healthz plus five /posts calls
 * (draft count, scheduled count, published count, archived count, recent list);
 * this routes each by URL.
 */
function dashboardFetch(
	opts: {
		draftTotal?: number;
		scheduledTotal?: number;
		publishedTotal?: number;
		archivedTotal?: number;
		recent?: ReturnType<typeof summary>[];
		health?: { ok: boolean };
	} = {}
) {
	const { draftTotal = 0, scheduledTotal = 0, publishedTotal = 0, archivedTotal = 0, recent = [], health = { ok: true } } = opts;
	return vi.fn(async (input: RequestInfo | URL) => {
		const url = typeof input === 'string' ? input : input.toString();
		if (url === '/healthz') {
			return { ok: health.ok, status: health.ok ? 200 : 503, json: async () => ({ status: 'ok', version: '0.1.0' }) };
		}
		if (url.includes('/posts')) {
			let posts: ReturnType<typeof summary>[] = [];
			let total = 0;
			if (url.includes('status=draft')) {
				total = draftTotal;
			} else if (url.includes('status=scheduled')) {
				total = scheduledTotal;
			} else if (url.includes('status=published')) {
				total = publishedTotal;
			} else if (url.includes('status=archived')) {
				total = archivedTotal;
			} else {
				posts = recent;
				total = recent.length;
			}
			const body = { posts, meta: { page: 1, per_page: 5, total_items: total, total_pages: 1 } };
			return { ok: true, status: 200, text: async () => JSON.stringify(body) };
		}
		throw new Error(`Unhandled request in test: ${url}`);
	});
}

describe('Dashboard page', () => {
	beforeEach(() => {
		auth.user = OWNER;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		auth.user = null;
	});

	it('shows draft, scheduled, published, and archived counts from the posts API', async () => {
		vi.stubGlobal('fetch', dashboardFetch({ draftTotal: 3, scheduledTotal: 2, publishedTotal: 5, archivedTotal: 1 }));

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Drafts')).toBeInTheDocument();
			expect(screen.getByText('3')).toBeInTheDocument();
		});
		expect(screen.getByText('Scheduled')).toBeInTheDocument();
		expect(screen.getByText('2')).toBeInTheDocument();
		expect(screen.getByText('Published')).toBeInTheDocument();
		expect(screen.getByText('5')).toBeInTheDocument();
		expect(screen.getByText('Archived')).toBeInTheDocument();
		expect(screen.getByText('1')).toBeInTheDocument();
	});

	it('lists recently edited posts linking to their editors', async () => {
		vi.stubGlobal(
			'fetch',
			dashboardFetch({
				recent: [
					summary({ id: 'p-1', title: 'First draft' }),
					summary({ id: 'p-2', title: 'Second draft' })
				]
			})
		);

		render(Page);

		const link = await screen.findByRole('link', { name: /First draft/ });
		expect(link).toHaveAttribute('href', '/posts/p-1');
		expect(screen.getByRole('link', { name: /Second draft/ })).toHaveAttribute('href', '/posts/p-2');
	});

	it('offers a prominent New post action', () => {
		vi.stubGlobal('fetch', dashboardFetch());

		render(Page);

		expect(screen.getByRole('link', { name: 'New post' })).toHaveAttribute('href', '/posts/new');
	});

	it('queries /healthz and displays API status and version', async () => {
		vi.stubGlobal('fetch', dashboardFetch());

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('ok')).toBeInTheDocument();
		});
		expect(screen.getByText('0.1.0')).toBeInTheDocument();
		expect(fetch).toHaveBeenCalledWith('/healthz');
	});

	it('shows an error state when the API is unreachable', async () => {
		vi.stubGlobal('fetch', dashboardFetch({ health: { ok: false } }));

		render(Page);

		await waitFor(() => {
			expect(screen.getByText(/Unable to reach the API/)).toBeInTheDocument();
		});
	});

	// The signed-in user's name, role badge and Logout button now live in the
	// persistent sidebar (rendered by the root layout, not this page). That
	// coverage moved to src/lib/components/nav/Sidebar.test.ts.
});

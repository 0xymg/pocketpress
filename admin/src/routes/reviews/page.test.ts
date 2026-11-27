import { render, screen, waitFor } from '@testing-library/svelte';
import { beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('$lib/api', () => ({
	ApiError: class ApiError extends Error {},
	listPosts: vi.fn(),
	approveReview: vi.fn(),
	requestChanges: vi.fn(),
	withdrawReview: vi.fn()
}));

import { listPosts } from '$lib/api';
import Page from './+page.svelte';

describe('/reviews page', () => {
	beforeEach(() => {
		vi.mocked(listPosts).mockReset();
	});

	it('lists pending review posts', async () => {
		vi.mocked(listPosts).mockResolvedValue({
			posts: [
				{
					id: 'p1',
					title: 'Needs eyes',
					slug: 'needs-eyes',
					status: 'draft',
					excerpt: '',
					featured: false,
					locale: 'en',
					author: { id: 'u1', name: 'Ada', slug: 'ada' },
					categories: [],
					tags: [],
					cover_image: null,
					og_image: null,
					scheduled_for: null,
					published_at: null,
					review_status: 'pending',
					review_note: '',
					reviewed_by: null,
					reviewed_at: null,
					submitted_for_review_at: '2026-11-27T12:00:00Z',
					created_at: '2026-11-27T11:00:00Z',
					updated_at: '2026-11-27T12:00:00Z'
				}
			],
			meta: { page: 1, per_page: 50, total_items: 1, total_pages: 1 }
		});

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Needs eyes')).toBeInTheDocument();
		});
		expect(listPosts).toHaveBeenCalledWith(
			expect.objectContaining({ review_status: 'pending' })
		);
		expect(screen.getByRole('button', { name: 'Approve' })).toBeInTheDocument();
	});

	it('shows empty state when nothing is pending', async () => {
		vi.mocked(listPosts).mockResolvedValue({
			posts: [],
			meta: { page: 1, per_page: 50, total_items: 0, total_pages: 0 }
		});

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('No posts pending review.')).toBeInTheDocument();
		});
	});
});

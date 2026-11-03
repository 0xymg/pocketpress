import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { mediaPicker } from '$lib/media-picker-state.svelte';
import MediaPicker from './MediaPicker.svelte';

function jsonResponse(status: number, ok: boolean, body: unknown) {
	return { ok, status, text: async () => JSON.stringify(body) };
}

const MEDIA_ITEM = {
	id: 'media-1',
	url: '/api/files/media-1/cover.jpg',
	filename: 'cover.jpg',
	mime: 'image/jpeg',
	size: 2048,
	width: 800,
	height: 600,
	alt: 'A cover shot',
	created_at: '2024-01-01T00:00:00Z'
};

function defaultFetchMock() {
	return vi.fn(async () =>
		jsonResponse(200, true, {
			media: [MEDIA_ITEM],
			meta: { page: 1, per_page: 40, total_items: 1, total_pages: 1 }
		})
	);
}

describe('MediaPicker — promise contract', () => {
	afterEach(() => {
		vi.unstubAllGlobals();
		// The store is a module singleton — leave it clean for the next test.
		if (mediaPicker.open) mediaPicker.cancel();
	});

	it('is closed (no dialog) until something calls pick()', () => {
		render(MediaPicker);
		expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
	});

	it('resolves pick() with the selected media item and closes', async () => {
		vi.stubGlobal('fetch', defaultFetchMock());
		render(MediaPicker);

		const pickPromise = mediaPicker.pick();

		await waitFor(() => {
			expect(screen.getByRole('dialog', { name: 'Select media' })).toBeInTheDocument();
		});
		await waitFor(() => {
			expect(screen.getByText('cover.jpg')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByText('cover.jpg'));

		await expect(pickPromise).resolves.toEqual(MEDIA_ITEM);
		expect(mediaPicker.open).toBe(false);
	});

	it('resolves pick() with null when Escape is pressed', async () => {
		vi.stubGlobal('fetch', defaultFetchMock());
		render(MediaPicker);

		const pickPromise = mediaPicker.pick();
		const dialog = await screen.findByRole('dialog', { name: 'Select media' });

		await fireEvent.keyDown(dialog, { key: 'Escape' });

		await expect(pickPromise).resolves.toBeNull();
		expect(mediaPicker.open).toBe(false);
	});

	it('resolves pick() with null on a backdrop click', async () => {
		vi.stubGlobal('fetch', defaultFetchMock());
		render(MediaPicker);

		const pickPromise = mediaPicker.pick();
		await screen.findByRole('dialog', { name: 'Select media' });

		await fireEvent.click(screen.getByRole('button', { name: 'Close media picker' }));

		await expect(pickPromise).resolves.toBeNull();
	});

	it('resolves pick() with null when the Close button is clicked', async () => {
		vi.stubGlobal('fetch', defaultFetchMock());
		render(MediaPicker);

		const pickPromise = mediaPicker.pick();
		await screen.findByRole('dialog', { name: 'Select media' });

		await fireEvent.click(screen.getByRole('button', { name: 'Close' }));

		await expect(pickPromise).resolves.toBeNull();
	});

	it('focuses the search input as soon as it opens', async () => {
		vi.stubGlobal('fetch', defaultFetchMock());
		render(MediaPicker);

		mediaPicker.pick();

		await waitFor(() => {
			expect(screen.getByLabelText('Search')).toHaveFocus();
		});
	});
});

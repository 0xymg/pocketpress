import { render, screen, waitFor } from '@testing-library/svelte';
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

const EDITOR = {
	id: 'editor-1',
	name: 'Grace Hopper',
	email: 'grace@example.com',
	role: 'editor' as const,
	active: true,
	last_login_at: null,
	created_at: '2024-01-01T00:00:00Z'
};

function mockUsersList(users: unknown[]) {
	return vi.fn().mockResolvedValue({
		ok: true,
		status: 200,
		text: async () => JSON.stringify({ users })
	});
}

describe('Users settings page', () => {
	afterEach(() => {
		vi.unstubAllGlobals();
	});

	it('renders the user list for an owner', async () => {
		auth.user = OWNER;
		vi.stubGlobal('fetch', mockUsersList([OWNER, EDITOR]));

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Grace Hopper')).toBeInTheDocument();
		});
		expect(screen.getByText('ada@example.com')).toBeInTheDocument();
		expect(screen.getByText('grace@example.com')).toBeInTheDocument();
	});

	it('blocks editors with a friendly no-access state and never calls the users API', async () => {
		auth.user = EDITOR;
		const fetchMock = mockUsersList([OWNER, EDITOR]);
		vi.stubGlobal('fetch', fetchMock);

		render(Page);

		expect(
			screen.getByText("You don't have access to this page. Ask an owner for access.")
		).toBeInTheDocument();
		expect(fetchMock).not.toHaveBeenCalled();
	});
});

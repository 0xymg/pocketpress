import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const gotoMock = vi.fn();
vi.mock('$app/navigation', () => ({
	goto: (...args: unknown[]) => gotoMock(...args)
}));

import { auth } from '$lib/auth.svelte';
import Sidebar from './Sidebar.svelte';

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
	slug: 'grace-hopper',
	bio: '',
	website: '',
	location: '',
	avatar: '',
	active: true,
	last_login_at: null,
	created_at: '2024-01-01T00:00:00Z'
};

describe('Sidebar', () => {
	beforeEach(() => {
		gotoMock.mockClear();
	});

	afterEach(() => {
		auth.user = null;
		vi.restoreAllMocks();
	});

	it('renders the Content group links for any signed-in user', () => {
		auth.user = EDITOR;
		render(Sidebar, { pathname: '/' });

		expect(screen.getByText('Content')).toBeInTheDocument();
		for (const [name, href] of [
			['Dashboard', '/'],
			['Posts', '/posts'],
			['Calendar', '/calendar'],
			['Reviews', '/reviews'],
			['Categories', '/categories'],
			['Tags', '/tags'],
			['Media', '/media'],
			['Profile', '/settings/profile']
		] as const) {
			expect(screen.getByRole('link', { name })).toHaveAttribute('href', href);
		}
	});

	it('shows the owner-only System group to owners', () => {
		auth.user = OWNER;
		render(Sidebar, { pathname: '/' });

		expect(screen.getByText('System')).toBeInTheDocument();
		expect(screen.getByRole('link', { name: 'Users' })).toHaveAttribute('href', '/settings/users');
		expect(screen.getByRole('link', { name: 'Settings' })).toHaveAttribute(
			'href',
			'/settings/general'
		);
		expect(screen.getByRole('link', { name: 'Webhooks' })).toHaveAttribute(
			'href',
			'/settings/webhooks'
		);
		expect(screen.getByRole('link', { name: 'API keys' })).toHaveAttribute(
			'href',
			'/settings/api-keys'
		);
	});

	it('hides the System group from editors', () => {
		auth.user = EDITOR;
		render(Sidebar, { pathname: '/' });

		expect(screen.queryByText('System')).not.toBeInTheDocument();
		expect(screen.queryByRole('link', { name: 'Users' })).not.toBeInTheDocument();
		expect(screen.queryByRole('link', { name: 'Webhooks' })).not.toBeInTheDocument();
	});

	it('marks the active item from the pathname with aria-current', () => {
		auth.user = OWNER;
		render(Sidebar, { pathname: '/posts/new' });

		expect(screen.getByRole('link', { name: 'Posts' })).toHaveAttribute('aria-current', 'page');
		expect(screen.getByRole('link', { name: 'Dashboard' })).not.toHaveAttribute('aria-current');
	});

	it('shows the signed-in user block with name, role badge and Logout', () => {
		auth.user = OWNER;
		render(Sidebar, { pathname: '/' });

		expect(screen.getByText('Ada Lovelace')).toBeInTheDocument();
		expect(screen.getByText('owner', { selector: '[data-slot="badge"]' })).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Logout' })).toBeInTheDocument();
	});

	it('logs out via auth.logout and redirects to /login', async () => {
		auth.user = OWNER;
		const logoutSpy = vi.spyOn(auth, 'logout').mockResolvedValue(undefined);

		render(Sidebar, { pathname: '/' });
		await fireEvent.click(screen.getByRole('button', { name: 'Logout' }));

		await waitFor(() => {
			expect(logoutSpy).toHaveBeenCalledTimes(1);
		});
		await waitFor(() => {
			expect(gotoMock).toHaveBeenCalledWith('/login');
		});
	});
});

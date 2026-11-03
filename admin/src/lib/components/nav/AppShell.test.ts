import { fireEvent, render, screen } from '@testing-library/svelte';
import { createRawSnippet } from 'svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

vi.mock('$app/navigation', () => ({
	goto: vi.fn()
}));

import { auth } from '$lib/auth.svelte';
import AppShell from './AppShell.svelte';

const OWNER = {
	id: 'owner-1',
	name: 'Ada Lovelace',
	email: 'ada@example.com',
	role: 'owner' as const,
	active: true,
	last_login_at: null,
	created_at: '2024-01-01T00:00:00Z'
};

const content = createRawSnippet(() => ({
	render: () => `<p data-testid="page-content">Page content</p>`
}));

describe('AppShell', () => {
	afterEach(() => {
		auth.user = null;
	});

	it('renders the page content it is given', () => {
		auth.user = OWNER;
		render(AppShell, { pathname: '/', children: content });

		expect(screen.getByTestId('page-content')).toBeInTheDocument();
	});

	it('mobile hamburger opens the drawer (backdrop appears); backdrop closes it', async () => {
		auth.user = OWNER;
		render(AppShell, { pathname: '/', children: content });

		// Drawer starts closed: no backdrop, hamburger is collapsed.
		const hamburger = screen.getByRole('button', { name: 'Open navigation' });
		expect(hamburger).toHaveAttribute('aria-expanded', 'false');
		expect(screen.queryByRole('button', { name: 'Close navigation' })).not.toBeInTheDocument();

		// Open it.
		await fireEvent.click(hamburger);
		expect(hamburger).toHaveAttribute('aria-expanded', 'true');
		const backdrop = screen.getByRole('button', { name: 'Close navigation' });
		expect(backdrop).toBeInTheDocument();

		// Close it via the backdrop.
		await fireEvent.click(backdrop);
		expect(screen.queryByRole('button', { name: 'Close navigation' })).not.toBeInTheDocument();
		expect(hamburger).toHaveAttribute('aria-expanded', 'false');
	});
});

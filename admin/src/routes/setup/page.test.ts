import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

const gotoMock = vi.fn();
vi.mock('$app/navigation', () => ({
	goto: (...args: unknown[]) => gotoMock(...args)
}));

import { auth } from '$lib/auth.svelte';
import Page from './+page.svelte';

function mockJsonResponse(status: number, ok: boolean, body: unknown) {
	return vi.fn().mockResolvedValue({
		ok,
		status,
		text: async () => JSON.stringify(body)
	});
}

async function fillValidForm() {
	await fireEvent.input(screen.getByLabelText('Site name'), { target: { value: 'My Blog' } });
	await fireEvent.input(screen.getByLabelText('Site URL'), {
		target: { value: 'https://example.com' }
	});
	await fireEvent.input(screen.getByLabelText('Owner name'), { target: { value: 'Ada Lovelace' } });
	await fireEvent.input(screen.getByLabelText('Owner email'), {
		target: { value: 'ada@example.com' }
	});
	await fireEvent.input(screen.getByLabelText('Password'), {
		target: { value: 'hunter2hunter2' }
	});
}

describe('Setup wizard', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.initialized = false;
		auth.user = null;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	it('renders every first-run setup field from plan section 9', () => {
		render(Page);

		expect(screen.getByLabelText('Site name')).toBeInTheDocument();
		expect(screen.getByLabelText('Site URL')).toBeInTheDocument();
		expect(screen.getByLabelText('Owner name')).toBeInTheDocument();
		expect(screen.getByLabelText('Owner email')).toBeInTheDocument();
		expect(screen.getByLabelText('Password')).toBeInTheDocument();
		expect(screen.getByLabelText('Timezone')).toBeInTheDocument();
	});

	it('pre-selects the browser-detected timezone', () => {
		render(Page);

		const select = screen.getByLabelText('Timezone') as HTMLSelectElement;
		expect(select.value).toBe(Intl.DateTimeFormat().resolvedOptions().timeZone);
	});

	it('blocks submission client-side when required fields are empty', async () => {
		vi.stubGlobal('fetch', vi.fn());

		render(Page);
		await fireEvent.click(screen.getByRole('button', { name: 'Create site' }));

		expect(screen.getByText('Site name is required.')).toBeInTheDocument();
		expect(fetch).not.toHaveBeenCalled();
		expect(gotoMock).not.toHaveBeenCalled();
	});

	it('renders per-field messages from a mocked 400 validation_failed response', async () => {
		vi.stubGlobal(
			'fetch',
			mockJsonResponse(400, false, {
				error: {
					code: 'validation_failed',
					message: 'Validation failed',
					details: {
						site_name: 'Site name is already taken',
						owner_email: 'Must be a valid email address'
					}
				}
			})
		);

		render(Page);
		await fillValidForm();
		await fireEvent.click(screen.getByRole('button', { name: 'Create site' }));

		await waitFor(() => {
			expect(screen.getByText('Site name is already taken')).toBeInTheDocument();
		});
		expect(screen.getByText('Must be a valid email address')).toBeInTheDocument();
		expect(gotoMock).not.toHaveBeenCalled();
	});

	it('refreshes auth state and redirects to /posts/new on success', async () => {
		vi.stubGlobal(
			'fetch',
			mockJsonResponse(201, true, {
				user: {
					id: '1',
					name: 'Ada Lovelace',
					email: 'ada@example.com',
					role: 'owner',
					slug: 'ada-lovelace',
					bio: '',
					website: '',
					location: '',
					avatar: '',
					active: true,
					last_login_at: null,
					created_at: '2024-01-01T00:00:00Z'
				}
			})
		);

		render(Page);
		await fillValidForm();
		await fireEvent.click(screen.getByRole('button', { name: 'Create site' }));

		await waitFor(() => {
			expect(gotoMock).toHaveBeenCalledWith('/posts/new');
		});
		expect(auth.initialized).toBe(true);
		expect(auth.user?.email).toBe('ada@example.com');
	});
});

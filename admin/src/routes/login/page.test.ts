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

describe('Login page', () => {
	beforeEach(() => {
		gotoMock.mockClear();
		auth.user = null;
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	it('renders the PocketPress heading and a real, enabled sign-in form', () => {
		render(Page);

		expect(screen.getByRole('heading', { name: 'PocketPress' })).toBeInTheDocument();
		expect(screen.getByLabelText('Email')).toBeEnabled();
		expect(screen.getByLabelText('Password')).toBeEnabled();
		expect(screen.getByRole('button', { name: 'Sign in' })).toBeEnabled();
	});

	it('stores the signed-in user and redirects to / on successful login', async () => {
		vi.stubGlobal(
			'fetch',
			mockJsonResponse(200, true, {
				user: {
					id: '1',
					name: 'Ada Lovelace',
					email: 'ada@example.com',
					role: 'owner',
					active: true,
					last_login_at: null,
					created_at: '2024-01-01T00:00:00Z'
				}
			})
		);

		render(Page);

		await fireEvent.input(screen.getByLabelText('Email'), {
			target: { value: 'ada@example.com' }
		});
		await fireEvent.input(screen.getByLabelText('Password'), {
			target: { value: 'correct-horse-battery-staple' }
		});
		await fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));

		await waitFor(() => {
			expect(gotoMock).toHaveBeenCalledWith('/');
		});
		expect(auth.user?.email).toBe('ada@example.com');
	});

	it('shows a single "Invalid email or password" message on a 401 response', async () => {
		vi.stubGlobal(
			'fetch',
			mockJsonResponse(401, false, {
				error: { code: 'invalid_credentials', message: 'email or password is incorrect' }
			})
		);

		render(Page);

		await fireEvent.input(screen.getByLabelText('Email'), {
			target: { value: 'ada@example.com' }
		});
		await fireEvent.input(screen.getByLabelText('Password'), { target: { value: 'wrong' } });
		await fireEvent.click(screen.getByRole('button', { name: 'Sign in' }));

		await waitFor(() => {
			expect(screen.getByRole('alert')).toHaveTextContent('Invalid email or password');
		});
		expect(gotoMock).not.toHaveBeenCalled();
		expect(auth.user).toBeNull();
	});
});

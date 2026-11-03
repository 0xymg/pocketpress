import { describe, expect, it } from 'vitest';
import { decideRoute } from './route-guard';

const user = { id: '1', name: 'Ada', email: 'ada@example.com', role: 'owner' as const };

describe('decideRoute', () => {
	it('does nothing while auth state is loading, regardless of route', () => {
		expect(
			decideRoute({ loading: true, initialized: false, user: null, pathname: '/' })
		).toBeNull();
		expect(
			decideRoute({ loading: true, initialized: true, user, pathname: '/settings/users' })
		).toBeNull();
	});

	it('redirects to /setup when uninitialized, from any other route', () => {
		expect(
			decideRoute({ loading: false, initialized: false, user: null, pathname: '/' })
		).toBe('/setup');
		expect(
			decideRoute({ loading: false, initialized: false, user: null, pathname: '/login' })
		).toBe('/setup');
	});

	it('does not redirect away from /setup when uninitialized', () => {
		expect(
			decideRoute({ loading: false, initialized: false, user: null, pathname: '/setup' })
		).toBeNull();
	});

	it('redirects to /login when initialized but signed out, from any other route', () => {
		expect(
			decideRoute({ loading: false, initialized: true, user: null, pathname: '/' })
		).toBe('/login');
		expect(
			decideRoute({
				loading: false,
				initialized: true,
				user: null,
				pathname: '/settings/users'
			})
		).toBe('/login');
	});

	it('sends signed-out visitors away from /setup once initialized, not to /setup', () => {
		expect(
			decideRoute({ loading: false, initialized: true, user: null, pathname: '/setup' })
		).toBe('/login');
	});

	it('does not redirect away from /login when initialized and signed out', () => {
		expect(
			decideRoute({ loading: false, initialized: true, user: null, pathname: '/login' })
		).toBeNull();
	});

	it('redirects signed-in users away from /login and /setup', () => {
		expect(
			decideRoute({ loading: false, initialized: true, user, pathname: '/login' })
		).toBe('/');
		expect(
			decideRoute({ loading: false, initialized: true, user, pathname: '/setup' })
		).toBe('/');
	});

	it('does not redirect signed-in users on ordinary routes', () => {
		expect(
			decideRoute({ loading: false, initialized: true, user, pathname: '/settings/users' })
		).toBeNull();
	});
});

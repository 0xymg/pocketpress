/**
 * Pure routing-guard logic for the admin SPA, extracted from +layout.svelte
 * so it can be unit tested without rendering Svelte components or mocking
 * $app/navigation.
 *
 * Rules (PocketPress_Development_Plan.md section 9 — first-run setup):
 *  - While auth state is still loading, do nothing (let the caller show a
 *    splash screen).
 *  - If the instance is not initialized, every route except /setup redirects
 *    to /setup.
 *  - Once initialized, /setup is unreachable — it redirects to /login (if
 *    signed out) or / (if signed in), mirroring server-side enforcement.
 *  - If initialized but signed out, every route except /login redirects to
 *    /login.
 *  - If signed in, /login is unreachable and redirects to /.
 */

export interface RouteGuardState {
	/** True while the initial setup-status/me bootstrap is in flight. */
	loading: boolean;
	/** Whether the instance has completed first-run setup. */
	initialized: boolean;
	/** The signed-in user, or null if signed out. */
	user: unknown | null;
	/** The current route pathname, e.g. "/settings/users". */
	pathname: string;
}

/**
 * Decides whether the current route should redirect elsewhere.
 * Returns the target pathname to redirect to, or null if the current route
 * is allowed to render as-is.
 */
export function decideRoute(state: RouteGuardState): string | null {
	if (state.loading) {
		return null;
	}

	if (!state.initialized) {
		return state.pathname === '/setup' ? null : '/setup';
	}

	if (!state.user) {
		if (state.pathname === '/setup') return '/login';
		return state.pathname === '/login' ? null : '/login';
	}

	// Signed in and initialized: /login and /setup are unreachable.
	if (state.pathname === '/login' || state.pathname === '/setup') {
		return '/';
	}

	return null;
}

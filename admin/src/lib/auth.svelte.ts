import { getMe, getSetupStatus, login as apiLogin, logout as apiLogout, submitSetup } from './api';
import type { LoginPayload, SetupPayload, User } from './api';

/**
 * Shared auth/session state for the admin SPA, expressed as Svelte 5 runes
 * on a singleton class instance (the idiomatic Svelte 5 replacement for
 * writable stores — see https://svelte.dev/docs/svelte/$state#Shared-state).
 *
 * src/routes/+layout.svelte drives `init()` once on mount and uses
 * `loading`/`initialized`/`user` (via src/lib/route-guard.ts's decideRoute)
 * to redirect between /setup, /login and the app.
 */
class AuthStore {
	/** True until the initial setup-status + me bootstrap has resolved. */
	loading = $state(true);
	/** Whether the instance has completed first-run setup. */
	initialized = $state(false);
	/** The signed-in user, or null if signed out. */
	user = $state<User | null>(null);
	/** Set only when the initial bootstrap request itself fails (e.g. API unreachable). */
	bootError = $state<string | null>(null);

	/**
	 * Resolves current auth state from the API: setup status, then (if
	 * initialized) the current session via /auth/me. Safe to call once on
	 * app mount; login/logout/setup update state locally afterwards so
	 * routine navigation never needs to re-fetch it.
	 */
	async init(): Promise<void> {
		this.loading = true;
		this.bootError = null;

		try {
			const status = await getSetupStatus();
			this.initialized = status.initialized;
		} catch (err) {
			// Backend unreachable at boot. Fall back to "not initialized" so the
			// UI has somewhere sane to land rather than hanging on the splash
			// screen forever; the setup/login forms will surface the real
			// error themselves on submit.
			this.initialized = false;
			this.bootError = err instanceof Error ? err.message : 'Unable to reach the API';
			this.user = null;
			this.loading = false;
			return;
		}

		if (this.initialized) {
			try {
				const { user } = await getMe();
				this.user = user;
			} catch {
				this.user = null;
			}
		} else {
			this.user = null;
		}

		this.loading = false;
	}

	async login(email: string, password: string): Promise<User> {
		const payload: LoginPayload = { email, password };
		const { user } = await apiLogin(payload);
		this.user = user;
		return user;
	}

	async logout(): Promise<void> {
		await apiLogout();
		this.user = null;
	}

	async setup(payload: SetupPayload): Promise<User> {
		const { user } = await submitSetup(payload);
		this.user = user;
		this.initialized = true;
		return user;
	}
}

export const auth = new AuthStore();

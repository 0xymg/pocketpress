import '@testing-library/jest-dom/vitest';
import { cleanup } from '@testing-library/svelte';
import { afterEach } from 'vitest';

// vite.config.ts runs vitest without globals, so @testing-library/svelte's
// built-in auto-cleanup (which hooks the global `afterEach`) never fires.
// Do it explicitly instead, so DOM from one test doesn't leak into the next.
afterEach(() => {
	cleanup();
	// The post editor autosaves drafts to localStorage; clear between tests so
	// one test's draft can't leak a restore bar into the next.
	try {
		localStorage.clear();
	} catch {
		// jsdom without localStorage — nothing to clear.
	}
});

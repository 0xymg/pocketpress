import { defineConfig, devices } from '@playwright/test';
import { BASE_URL } from './support/constants';

/**
 * PocketPress e2e suite (plan section 14 / Phase 8). Single chromium
 * project, single spec: the golden path (setup → CRUD → publish → public
 * API → webhook → edit → revision → unpublish → 404).
 *
 * support/global-setup.ts builds (if needed) and starts a throwaway
 * `pocketpress serve` on BASE_URL against a fresh temp data dir, and tears
 * it down after the run. The webhook worker (internal/webhook/worker.go)
 * ticks every 5s, so delivery assertions poll for up to ~15s — see
 * tests/golden-path.spec.ts.
 */
export default defineConfig({
  testDir: './tests',
  timeout: 120_000,
  expect: {
    timeout: 10_000,
  },
  fullyParallel: false,
  workers: 1,
  retries: 0,
  reporter: [['list']],
  globalSetup: './support/global-setup.ts',
  use: {
    baseURL: BASE_URL,
    trace: 'retain-on-failure',
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
});

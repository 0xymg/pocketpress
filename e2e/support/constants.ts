/**
 * Shared constants for the e2e harness. Imported by playwright.config.ts,
 * support/global-setup.ts and the spec itself so the address/secret are
 * defined in exactly one place.
 */

/** Host:port the throwaway PocketPress instance listens on (never 8090 — that's the user's live server). */
export const HTTP_ADDR = '127.0.0.1:8123';
export const BASE_URL = `http://${HTTP_ADDR}`;

/** In-process webhook receiver (started by the spec itself — see support/webhook-receiver.ts). */
export const WEBHOOK_HOST = '127.0.0.1';
export const WEBHOOK_PORT = 8124;
export const WEBHOOK_PATH = '/hook';
export const WEBHOOK_URL = `http://${WEBHOOK_HOST}:${WEBHOOK_PORT}${WEBHOOK_PATH}`;

/** PP_APP_SECRET for the throwaway instance — fixed so failures are reproducible. */
export const APP_SECRET = 'e2e-secret-0123456789abcdef0123456789abcdef';

/** Env var support/global-setup.ts uses to hand the temp data dir's absolute path to the spec (for the sqlite3 revision check). */
export const DATA_DIR_ENV = 'PP_E2E_DATA_DIR';

/** Golden-path fixture data (plan section 14). */
export const SITE_NAME = 'E2E Blog';
export const SITE_URL = BASE_URL;
export const OWNER_NAME = 'E2E Owner';
export const OWNER_EMAIL = 'e2e@example.com';
export const OWNER_PASSWORD = 'E2ePassw0rd!2026';
export const SETUP_TIMEZONE = 'UTC';

export const CATEGORY_NAME = 'News';
export const TAG_NAME = 'release';

export const WEBHOOK_NAME = 'E2E Webhook';

export const POST_TITLE = 'Hello E2E World';
export const POST_SLUG = 'hello-e2e-world';
export const POST_TITLE_EDITED = 'Hello E2E World (edited)';
export const PARAGRAPH_TEXT =
  'This paragraph was typed into the Lexical editor by the PocketPress end-to-end test.';

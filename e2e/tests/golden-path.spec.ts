import { execFileSync } from 'node:child_process';
import path from 'node:path';
import { expect, test } from '@playwright/test';
import {
  CATEGORY_NAME,
  DATA_DIR_ENV,
  OWNER_EMAIL,
  OWNER_NAME,
  OWNER_PASSWORD,
  PARAGRAPH_TEXT,
  POST_SLUG,
  POST_TITLE,
  POST_TITLE_EDITED,
  SETUP_TIMEZONE,
  SITE_NAME,
  SITE_URL,
  TAG_NAME,
  WEBHOOK_HOST,
  WEBHOOK_NAME,
  WEBHOOK_PORT,
  WEBHOOK_URL,
} from '../support/constants';
import { makeTinyPng } from '../support/png';
import { isValidSignature } from '../support/signature';
import { pollUntil } from '../support/wait';
import { createWebhookReceiver, type WebhookReceiver } from '../support/webhook-receiver';

/**
 * The golden path (PocketPress_Development_Plan.md section 14 / Phase 8):
 * setup → category+tag → webhook → post+image → publish → public API →
 * webhook delivery → edit → revision → unpublish → 404. Driven through the
 * real admin UI (selectors read from admin/src) wherever a UI exists; the
 * only exceptions are noted inline with a REST-fallback comment.
 *
 * support/global-setup.ts (Playwright's globalSetup) builds/starts a
 * throwaway `pocketpress serve` against a fresh temp data dir before this
 * file runs, and tears it down after. The webhook receiver below runs
 * in-process (this test file), per the task brief.
 */

let receiver: WebhookReceiver;

test.beforeAll(async () => {
  receiver = await createWebhookReceiver(WEBHOOK_HOST, WEBHOOK_PORT);
});

test.afterAll(async () => {
  await receiver.close();
});

function dashboardCount(page: import('@playwright/test').Page, label: string) {
  return page.locator(`p:text-is("${label}") + p`);
}

test('golden path: setup → CRUD → publish → public API → webhook → edit → revision → unpublish → 404', async ({
  page,
  request,
}) => {
  test.setTimeout(120_000);

  // Auto-accept the one confirm() this app can show (PostEditor's
  // unsaved-changes guard, admin/src/lib/components/posts/PostEditor.svelte)
  // — the golden path always Saves before navigating away, so this dialog is
  // not expected to fire; it's a safety net against a hang if it ever does.
  page.on('dialog', (dialog) => dialog.accept());

  let webhookSecret = '';

  await test.step('1) fresh instance redirects to /setup; complete the wizard', async () => {
    await page.goto('/');
    await page.waitForURL(/\/setup$/);
    await expect(page.getByRole('heading', { name: 'Set up PocketPress' })).toBeVisible();

    await page.getByLabel('Site name').fill(SITE_NAME);
    await page.getByLabel('Site URL').fill(SITE_URL);
    await page.getByLabel('Owner name').fill(OWNER_NAME);
    await page.getByLabel('Owner email').fill(OWNER_EMAIL);
    await page.getByLabel('Password').fill(OWNER_PASSWORD);
    await page.getByLabel('Timezone').selectOption(SETUP_TIMEZONE);
    await page.getByRole('button', { name: 'Create site' }).click();

    // Setup creates the owner, signs them in, and lands on /posts/new
    // (plan section 9 "First-run setup"). PostEditor.svelte has its own
    // Payload-style document header — leave it via the "Posts" breadcrumb
    // (scoped to the breadcrumb nav so it doesn't collide with the sidebar's
    // "Posts" link) to reach a screen where we assert we're signed in.
    await page.waitForURL(/\/posts\/new$/);
    await page
      .getByRole('navigation', { name: 'Breadcrumb' })
      .getByRole('link', { name: 'Posts' })
      .click();
    await page.waitForURL(/\/posts$/);
    await expect(page.getByRole('button', { name: /Logout/ })).toBeVisible();

    // Sanity: a fresh instance's dashboard shows zero everything.
    await page.getByRole('link', { name: 'Dashboard' }).click();
    await page.waitForURL(/\/$/);
    await expect(dashboardCount(page, 'Drafts')).toHaveText('0');
    await expect(dashboardCount(page, 'Published')).toHaveText('0');
  });

  await test.step('2) create category "News" and tag "release" via their pages', async () => {
    await page.getByRole('link', { name: 'Categories' }).click();
    await page.waitForURL(/\/categories$/);
    await page.getByLabel('Name').fill(CATEGORY_NAME);
    await page.getByRole('button', { name: 'New category' }).click();
    await expect(page.getByText(CATEGORY_NAME, { exact: true })).toBeVisible();
    await expect(page.getByText('/news', { exact: true })).toBeVisible();

    await page.getByRole('link', { name: 'Tags' }).click();
    await page.waitForURL(/\/tags$/);
    await page.getByLabel('Name').fill(TAG_NAME);
    await page.getByRole('button', { name: 'New tag' }).click();
    await expect(page.getByText(TAG_NAME, { exact: true })).toBeVisible();
    await expect(page.getByText('/release', { exact: true })).toBeVisible();
  });

  await test.step('3) create webhook via /settings/webhooks and capture the one-time secret', async () => {
    await page.getByRole('link', { name: 'Webhooks' }).click();
    await page.waitForURL(/\/settings\/webhooks$/);

    await page.getByLabel('Name').fill(WEBHOOK_NAME);
    await page.getByLabel('URL').fill(WEBHOOK_URL);
    await page.getByLabel('Post published').check();
    await page.getByRole('button', { name: 'New webhook' }).click();

    // The secret ("whsec_...", internal/webhook/crypto.go) is shown exactly
    // once, in the reveal panel — capture it now, we'll need it to verify
    // the delivery signature in step 7.
    const secretCode = page.locator('code', { hasText: /^whsec_/ });
    await expect(secretCode).toBeVisible();
    webhookSecret = (await secretCode.innerText()).trim();
    expect(webhookSecret).toMatch(/^whsec_/);

    await page.getByRole('button', { name: "I've stored it — dismiss" }).click();
    await expect(page.getByText(WEBHOOK_NAME, { exact: true })).toBeVisible();
    await expect(page.getByText(WEBHOOK_URL, { exact: true })).toBeVisible();
  });

  await test.step('4) new post: title, paragraph, inline image, category+tag+cover, save', async () => {
    await page.getByRole('link', { name: 'Posts' }).click();
    await page.waitForURL(/\/posts$/);
    await page.getByRole('link', { name: 'New post' }).click();
    await page.waitForURL(/\/posts\/new$/);

    await page.getByLabel('Title', { exact: true }).fill(POST_TITLE);
    // Slug auto-fills from the title for a new, untouched post
    // (admin/src/lib/post-editor-state.svelte.ts setTitle()).
    await expect(page.getByLabel('Slug')).toHaveValue(POST_SLUG);

    const editor = page.getByRole('textbox', { name: 'Content' });
    await editor.click();
    await editor.pressSequentially(PARAGRAPH_TEXT, { delay: 8 });
    // LexicalEditor.svelte debounces onChange ~300ms before it reaches
    // formState; give it a moment before anything reads formState (Save).
    await page.waitForTimeout(500);

    // Insert an image through the editor's Payload-style block menu (the "+"
    // gutter inserter → "Image"), which opens the same media picker as the
    // cover/OG image fields (PostEditor wires onInsertImage to it). This
    // replaced the old fixed-toolbar "Insert image" button when the editor
    // adopted Payload's floating-toolbar + block-menu UX.
    const pngBuffer = makeTinyPng();
    await page.getByRole('button', { name: 'Add block' }).click();
    await page.getByRole('menuitem', { name: 'Image' }).click();
    const mediaDialog = page.getByRole('dialog', { name: 'Select media' });
    await expect(mediaDialog).toBeVisible();

    const uploadResponse = page.waitForResponse(
      (res) => res.url().includes('/api/admin/v1/media') && res.request().method() === 'POST'
    );
    await mediaDialog.getByLabel('Upload media').setInputFiles({
      name: 'e2e-cover.png',
      mimeType: 'image/png',
      buffer: pngBuffer,
    });
    const uploadRes = await uploadResponse;
    expect(uploadRes.ok(), 'media upload should succeed').toBeTruthy();

    const uploadedThumb = mediaDialog.locator('button:has(img)').first();
    await expect(uploadedThumb).toBeVisible();
    await uploadedThumb.click();
    await expect(mediaDialog).toBeHidden();

    await page.getByLabel(CATEGORY_NAME).check();
    await page.getByLabel(TAG_NAME).check();

    // Cover image: reopen the same picker and reuse the image just uploaded.
    await page.getByRole('button', { name: 'Pick cover image' }).click();
    await expect(mediaDialog).toBeVisible();
    const existingThumb = mediaDialog.locator('button:has(img)').first();
    await expect(existingThumb).toBeVisible();
    await existingThumb.click();
    await expect(mediaDialog).toBeHidden();
    await expect(page.getByRole('button', { name: 'Change cover image' })).toBeVisible();

    await page.getByRole('button', { name: 'Save', exact: true }).click();
    // /posts/new -> /posts/{id} swaps to a different route component
    // (routes/posts/new vs routes/posts/[id]), so PostEditor remounts here
    // and its local `savedAt` toast resets — assert the freshly-loaded post
    // instead (title round-tripped, draft action bar present).
    await page.waitForURL(/\/posts\/(?!new$)[^/]+$/);
    await expect(page.getByLabel('Title', { exact: true })).toHaveValue(POST_TITLE);
    // Draft doc-controls: Save + the primary "Publish" button (secondary actions
    // like Schedule/Archive/Delete now live in the "..." menu, Payload-style).
    await expect(page.getByRole('button', { name: 'Publish', exact: true })).toBeVisible();

    // Sanity: media library lists the upload. Leave the editor via the
    // "Posts" breadcrumb (scoped to the breadcrumb nav) first.
    await page
      .getByRole('navigation', { name: 'Breadcrumb' })
      .getByRole('link', { name: 'Posts' })
      .click();
    await page.waitForURL(/\/posts$/);
    await page.getByRole('link', { name: 'Media' }).click();
    await page.waitForURL(/\/media$/);
    expect(await page.locator('button:has(img)').count()).toBeGreaterThanOrEqual(1);
    await expect(page.getByText('No media yet', { exact: false })).toHaveCount(0);

    // Sanity: dashboard now shows one draft, listed as recently edited.
    await page.getByRole('link', { name: 'Dashboard' }).click();
    await page.waitForURL(/\/$/);
    await expect(dashboardCount(page, 'Drafts')).toHaveText('1');
    const recentLink = page.getByRole('link', { name: new RegExp(POST_TITLE) });
    await expect(recentLink).toBeVisible();

    // Back into the editor to publish.
    await recentLink.click();
    await page.waitForURL(/\/posts\/(?!new$)[^/]+$/);
  });

  await test.step('5) publish via the action bar (confirm)', async () => {
    await page.getByRole('button', { name: 'Publish', exact: true }).click();
    await expect(page.getByText('Publish now?')).toBeVisible();
    await page.getByRole('button', { name: 'Confirm' }).click();
    // Once published the primary "Publish" button is gone (Unpublish moved into
    // the "..." menu); the sidebar status meta now reads "published".
    await expect(page.getByRole('button', { name: 'Publish', exact: true })).toHaveCount(0);
    await expect(page.getByText('published', { exact: true })).toBeVisible();
  });

  await test.step('6) poll the public API until the post is live, no cookie', async () => {
    // `request` is a fresh APIRequestContext, independent of `page`'s
    // session cookie — exactly the "no cookie" public-reader view.
    const res = await pollUntil(
      async () => {
        const r = await request.get(`/api/v1/posts/${POST_SLUG}`);
        return r.ok() ? r : false;
      },
      { timeoutMs: 10_000, intervalMs: 400, timeoutMessage: `GET /api/v1/posts/${POST_SLUG} never returned 200` }
    );
    // internal/http/publicapi/posts.go wraps the single-post response as {"data": {...}}.
    const { data: body } = await res.json();
    expect(body.title).toBe(POST_TITLE);
    expect(body.content_html).toContain(PARAGRAPH_TEXT);
    expect(body.cover_image).not.toBeNull();
    expect(typeof body.cover_image.url).toBe('string');
    expect(body.cover_image.url).toContain('/api/files/');
  });

  await test.step('7) poll the webhook receiver for a validly-signed post.published delivery', async () => {
    // The delivery worker (internal/webhook/worker.go) ticks every 5s; allow
    // up to three ticks.
    const delivery = await pollUntil(
      async () => receiver.deliveries.find((d) => d.event === 'post.published') ?? false,
      {
        timeoutMs: 15_000,
        intervalMs: 300,
        timeoutMessage: 'No post.published webhook delivery received within 15s',
      }
    );

    expect(delivery.timestamp, 'X-PocketPress-Timestamp header').toBeTruthy();
    expect(delivery.signature, 'X-PocketPress-Signature header').toBeTruthy();
    expect(
      isValidSignature(webhookSecret, delivery.timestamp as string, delivery.rawBody, delivery.signature as string),
      'HMAC signature must verify against the captured secret'
    ).toBe(true);

    const payload = delivery.payload as { event: string; data: { post: { slug: string; status: string } } };
    expect(payload.event).toBe('post.published');
    expect(payload.data.post.slug).toBe(POST_SLUG);
    expect(payload.data.post.status).toBe('published');
  });

  await test.step('8) edit the post title and save', async () => {
    await page.getByLabel('Title', { exact: true }).fill(POST_TITLE_EDITED);
    await page.getByRole('button', { name: 'Save', exact: true }).click();
    await expect(page.getByText(/^Saved \d{2}:\d{2}$/)).toBeVisible();
  });

  await test.step('9) confirm a revision exists (sqlite3 CLI against the temp data dir)', async () => {
    const dataDir = process.env[DATA_DIR_ENV];
    if (!dataDir) {
      throw new Error(`${DATA_DIR_ENV} is not set — did support/global-setup.ts run?`);
    }
    const dbPath = path.join(dataDir, 'pocketpress.db');
    const sql = `SELECT COUNT(*) FROM post_revisions WHERE post_id = (SELECT id FROM posts WHERE slug = '${POST_SLUG}');`;
    const output = execFileSync('sqlite3', ['-readonly', dbPath, sql], { encoding: 'utf8' }).trim();
    const revisionCount = Number(output);
    expect(Number.isFinite(revisionCount), `sqlite3 output was not a number: ${output}`).toBe(true);
    // Revision #1 from CreatePost, #2 from the title edit in step 8
    // (internal/content/service.go maybeCreateRevision).
    expect(revisionCount).toBeGreaterThanOrEqual(2);
  });

  await test.step('10) unpublish via the "..." menu', async () => {
    // Unpublish is a secondary action, now inside the doc-controls "..." menu.
    await page.getByRole('button', { name: 'More actions' }).click();
    await page.getByRole('button', { name: 'Unpublish' }).click();
    // Back to draft → the primary "Publish" button returns.
    await expect(page.getByRole('button', { name: 'Publish', exact: true })).toBeVisible();
  });

  await test.step('11) public endpoint now returns 404', async () => {
    const res = await pollUntil(
      async () => {
        const r = await request.get(`/api/v1/posts/${POST_SLUG}`);
        return r.status() === 404 ? r : false;
      },
      {
        timeoutMs: 10_000,
        intervalMs: 400,
        timeoutMessage: `GET /api/v1/posts/${POST_SLUG} never returned 404 after unpublish`,
      }
    );
    const body = await res.json();
    expect(body.error.code).toBe('post_not_found');
  });
});

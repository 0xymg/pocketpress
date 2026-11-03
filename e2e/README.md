# PocketPress e2e

Playwright suite covering the golden path from
`PocketPress_Development_Plan.md` section 14: setup → categories/tags →
webhook → post with image → publish → public API → webhook delivery → edit →
revision → unpublish → 404. One spec, driven through the real admin UI
(`admin/src`) wherever a UI exists.

## Run it

```bash
cd e2e
npm ci
npx playwright install chromium
npm test
```

`npm test` (`playwright test`) does the rest automatically:

- `support/global-setup.ts` builds `../pocketpress` if missing or stale
  (`make build` from the repo root), otherwise reuses it.
- It starts `../pocketpress serve` on `127.0.0.1:8123` against a fresh temp
  data dir under `e2e/.tmp/` (`PP_ENV=development`, a fixed
  `PP_APP_SECRET`), waits for `/healthz`, and tears the process + data dir
  down after the run — it never touches `pp_data/` or port 8090.
- The spec itself starts an in-process webhook receiver on
  `127.0.0.1:8124` (`support/webhook-receiver.ts`).

Run headed to watch it: `npm run test:headed`.

## Layout

- `tests/golden-path.spec.ts` — the one spec.
- `support/global-setup.ts` — builds/starts/stops the PocketPress instance.
- `support/webhook-receiver.ts` — records raw webhook deliveries.
- `support/signature.ts` — HMAC-SHA256 verification (`node:crypto`), mirrors
  `internal/webhook/sign.go`.
- `support/png.ts` — from-scratch PNG encoder for the upload fixture (the
  server decodes the image, so it must be a real one).
- `support/constants.ts` — shared addresses/fixture data.

## Notes

- Single chromium project; `playwright.config.ts` sets a 120s test timeout
  since the webhook worker (`internal/webhook/worker.go`) ticks every 5s and
  delivery assertions poll for up to 15s.
- `trace: 'retain-on-failure'` — inspect a failing trace with
  `npx playwright show-trace test-results/**/trace.zip`.
- Nothing here is committed to the Go module or `admin/`; this package has
  its own `package.json`/lockfile and is gitignored (`.tmp/`,
  `node_modules/`, `test-results/`, `playwright-report/`).

import { type ChildProcess, execFileSync, spawn } from 'node:child_process';
import { existsSync, mkdirSync, readdirSync, rmSync, statSync } from 'node:fs';
import net from 'node:net';
import path from 'node:path';
import { APP_SECRET, BASE_URL, DATA_DIR_ENV, HTTP_ADDR, WEBHOOK_PORT } from './constants';
import { sleep } from './wait';

const E2E_DIR = path.resolve(__dirname, '..');
const REPO_ROOT = path.resolve(E2E_DIR, '..');
const BINARY_PATH = path.join(REPO_ROOT, 'pocketpress');

// Inputs that, if newer than the built ../pocketpress binary, mean it's
// stale and must be rebuilt. Deliberately excludes generated output
// (admin/dist, node_modules, .svelte-kit) so the build's own output is never
// mistaken for a reason to rebuild itself.
const SOURCE_ROOTS = [
  'cmd',
  'internal',
  'migrations',
  'go.mod',
  'go.sum',
  'Makefile',
  'admin/src',
  'admin/static',
  'admin/package.json',
  'admin/package-lock.json',
  'admin/vite.config.ts',
  'admin/tsconfig.json',
  'admin/components.json',
  'admin/embed.go',
];

function newestMtimeMs(target: string): number {
  const st = statSync(target);
  if (!st.isDirectory()) return st.mtimeMs;
  let newest = st.mtimeMs;
  for (const entry of readdirSync(target)) {
    if (entry === 'node_modules' || entry === '.svelte-kit' || entry === 'dist' || entry === '.git') continue;
    newest = Math.max(newest, newestMtimeMs(path.join(target, entry)));
  }
  return newest;
}

function binaryIsFresh(): boolean {
  if (!existsSync(BINARY_PATH)) return false;
  const binMtime = statSync(BINARY_PATH).mtimeMs;
  for (const rel of SOURCE_ROOTS) {
    const p = path.join(REPO_ROOT, rel);
    if (!existsSync(p)) continue;
    if (newestMtimeMs(p) > binMtime) return false;
  }
  return true;
}

/** `make build` (from repo root) if missing/stale; otherwise reuse ../pocketpress as-is. Never touches pp_data/ or the live :8090 server — this only (re)compiles the shared binary file. */
function ensureBinary(): void {
  if (binaryIsFresh()) {
    console.log('[e2e:setup] Reusing existing ../pocketpress binary (already up to date).');
    return;
  }
  console.log('[e2e:setup] ../pocketpress is missing or stale — running `make build`...');
  const start = Date.now();
  execFileSync('make', ['build'], { cwd: REPO_ROOT, stdio: 'inherit' });
  console.log(`[e2e:setup] make build finished in ${((Date.now() - start) / 1000).toFixed(1)}s`);
}

/** Fails fast (instead of silently reusing a leftover process) if a prior e2e run leaked a listener on this port. */
function checkPortFree(port: number): Promise<void> {
  return new Promise((resolve, reject) => {
    const socket = net.createConnection({ host: '127.0.0.1', port }, () => {
      socket.destroy();
      reject(
        new Error(
          `Port ${port} is already in use — a leftover process from a previous e2e run may still be alive. ` +
            `Find and stop it (e.g. \`lsof -nP -iTCP:${port} -sTCP:LISTEN\`) before re-running the suite.`
        )
      );
    });
    socket.once('error', () => {
      socket.destroy();
      resolve();
    });
  });
}

async function waitForHealthz(timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs;
  let lastError: unknown;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${BASE_URL}/healthz`);
      if (res.ok) return;
      lastError = new Error(`/healthz returned ${res.status}`);
    } catch (err) {
      lastError = err;
    }
    await sleep(200);
  }
  throw new Error(`Timed out waiting for ${BASE_URL}/healthz to come up: ${String(lastError)}`);
}

async function stopChild(child: ChildProcess, name: string, timeoutMs = 5000): Promise<void> {
  if (child.exitCode !== null || child.signalCode !== null) return;
  let exited = false;
  const exitedPromise = new Promise<void>((resolve) =>
    child.once('exit', () => {
      exited = true;
      resolve();
    })
  );
  child.kill('SIGTERM');
  await Promise.race([exitedPromise, sleep(timeoutMs)]);
  if (!exited) {
    console.warn(`[e2e:teardown] ${name} did not exit within ${timeoutMs}ms of SIGTERM — sending SIGKILL`);
    child.kill('SIGKILL');
    await exitedPromise;
  }
  console.log(`[e2e:teardown] ${name} stopped.`);
}

/**
 * Playwright globalSetup (see playwright.config.ts): builds the binary if
 * needed, starts `../pocketpress serve` against a fresh temp data dir under
 * e2e/.tmp/ with PP_ENV=development and a fixed PP_APP_SECRET, and waits for
 * /healthz. This process (the Playwright CLI's main process) stays alive for
 * the whole `npx playwright test` run, so the returned teardown closure —
 * called once after every test finishes — reliably kills the child and wipes
 * the temp dir. Never binds port 8090 and never touches pp_data/ — those
 * belong to the user's separately-running live instance.
 */
export default async function globalSetup(): Promise<() => Promise<void>> {
  await checkPortFree(8123);
  await checkPortFree(WEBHOOK_PORT);

  ensureBinary();

  const runId = `${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  const dataDir = path.join(E2E_DIR, '.tmp', `data-${runId}`);
  mkdirSync(dataDir, { recursive: true });

  console.log(`[e2e:setup] Starting ../pocketpress serve on ${HTTP_ADDR} (data dir: ${dataDir})`);
  const server = spawn(BINARY_PATH, ['serve'], {
    cwd: REPO_ROOT,
    env: {
      ...process.env,
      PP_ENV: 'development',
      PP_HTTP_ADDR: HTTP_ADDR,
      PP_DATA_DIR: dataDir,
      PP_APP_SECRET: APP_SECRET,
    },
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  server.stdout?.on('data', (chunk: Buffer) => process.stdout.write(`[pocketpress] ${chunk}`));
  server.stderr?.on('data', (chunk: Buffer) => process.stderr.write(`[pocketpress] ${chunk}`));
  server.once('exit', (code, signal) => {
    console.log(`[e2e] pocketpress serve exited (code=${code}, signal=${signal})`);
  });

  try {
    await waitForHealthz(20_000);
  } catch (err) {
    await stopChild(server, 'pocketpress serve');
    throw err;
  }
  console.log('[e2e:setup] pocketpress is healthy.');

  // Handed to the spec (a different Node process) via env — set here, before
  // Playwright forks test workers, so they inherit it.
  process.env[DATA_DIR_ENV] = dataDir;

  return async function globalTeardown() {
    await stopChild(server, 'pocketpress serve');
    try {
      rmSync(dataDir, { recursive: true, force: true });
    } catch {
      // best-effort cleanup — trace files etc. are more useful than a clean .tmp/ on failure
    }
  };
}

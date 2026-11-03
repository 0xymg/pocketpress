/** Small polling helpers shared by global-setup and the spec. */

export function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

/**
 * Calls `check` every `intervalMs` until it returns a truthy value or
 * `timeoutMs` elapses, in which case it throws `new Error(timeoutMessage)`.
 * Returns whatever truthy value `check` produced.
 */
export async function pollUntil<T>(
  check: () => Promise<T | undefined | null | false>,
  opts: { timeoutMs: number; intervalMs?: number; timeoutMessage: string }
): Promise<T> {
  const intervalMs = opts.intervalMs ?? 300;
  const deadline = Date.now() + opts.timeoutMs;
  let lastError: unknown;
  while (Date.now() < deadline) {
    try {
      const result = await check();
      if (result) return result;
    } catch (err) {
      lastError = err;
    }
    await sleep(intervalMs);
  }
  const suffix = lastError ? ` (last error: ${String(lastError)})` : '';
  throw new Error(opts.timeoutMessage + suffix);
}

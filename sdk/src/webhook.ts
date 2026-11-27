import { createHmac, timingSafeEqual } from "node:crypto";

export interface VerifyWebhookOptions {
  /** Value of the `X-PocketPress-Signature` header (e.g. `sha256=...`). */
  signature: string | null | undefined;
  /** Value of the `X-PocketPress-Timestamp` header. */
  timestamp: string | null | undefined;
  /** Raw request body as a string (must be the exact bytes PocketPress signed). */
  body: string;
  /** Webhook signing secret from the PocketPress admin. */
  secret: string;
  /**
   * Maximum allowed age of the timestamp in seconds (default 300).
   * Set to `0` or a negative number to skip the skew check.
   */
  maxAgeSeconds?: number;
  /** Clock used for skew checks; defaults to Date.now(). */
  now?: () => number;
}

/**
 * Verify a PocketPress webhook HMAC signature.
 *
 * PocketPress signs `timestamp + "." + body` with HMAC-SHA256 and sends
 * `X-PocketPress-Signature: sha256=<hex>` plus `X-PocketPress-Timestamp`.
 *
 * Returns `true` when the signature is valid and (by default) the timestamp
 * is within `maxAgeSeconds`. Node.js / Bun only (uses `node:crypto`).
 */
export function verifyWebhookSignature(options: VerifyWebhookOptions): boolean {
  const {
    signature,
    timestamp,
    body,
    secret,
    maxAgeSeconds = 300,
    now = Date.now,
  } = options;

  if (!signature || !timestamp || !secret) {
    return false;
  }

  const prefix = "sha256=";
  if (!signature.startsWith(prefix)) {
    return false;
  }
  const providedHex = signature.slice(prefix.length);
  if (!/^[0-9a-f]+$/i.test(providedHex) || providedHex.length % 2 !== 0) {
    return false;
  }

  if (maxAgeSeconds > 0) {
    const ts = Number(timestamp);
    if (!Number.isFinite(ts)) {
      return false;
    }
    // Accept seconds or milliseconds timestamps.
    const tsMs = ts > 1e12 ? ts : ts * 1000;
    if (Math.abs(now() - tsMs) > maxAgeSeconds * 1000) {
      return false;
    }
  }

  const expectedHex = createHmac("sha256", secret)
    .update(`${timestamp}.${body}`)
    .digest("hex");

  try {
    const a = Buffer.from(providedHex, "hex");
    const b = Buffer.from(expectedHex, "hex");
    if (a.length !== b.length) {
      return false;
    }
    return timingSafeEqual(a, b);
  } catch {
    return false;
  }
}

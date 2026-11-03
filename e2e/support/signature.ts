import { createHmac, timingSafeEqual } from 'node:crypto';

/**
 * Mirrors internal/webhook/sign.go: sha256=hex(HMAC-SHA256(secret, timestamp + "." + raw_body))
 * (plan section 8). Used by the spec to verify a delivery the in-process
 * webhook receiver recorded (support/webhook-receiver.ts) once it has the
 * one-time secret captured from the /settings/webhooks reveal panel.
 */
export function computeSignature(secret: string, timestamp: string, rawBody: string): string {
  const mac = createHmac('sha256', secret).update(`${timestamp}.${rawBody}`).digest('hex');
  return `sha256=${mac}`;
}

export function isValidSignature(
  secret: string,
  timestamp: string,
  rawBody: string,
  signatureHeader: string
): boolean {
  const expected = Buffer.from(computeSignature(secret, timestamp, rawBody));
  const actual = Buffer.from(signatureHeader);
  if (expected.length !== actual.length) return false;
  return timingSafeEqual(expected, actual);
}

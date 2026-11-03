import http from 'node:http';
import type { AddressInfo } from 'node:net';

/** One recorded delivery attempt (plan section 8 headers, raw body kept verbatim for HMAC verification). */
export interface RecordedDelivery {
  receivedAt: string;
  event: string | null;
  deliveryId: string | null;
  timestamp: string | null;
  signature: string | null;
  rawBody: string;
  payload: unknown;
}

export interface WebhookReceiver {
  deliveries: RecordedDelivery[];
  close(): Promise<void>;
}

/**
 * A tiny webhook receiver that lives inside the Playwright test process
 * itself (plan section 14 step 7 / task brief item 3) — started in
 * test.beforeAll and stopped in test.afterAll in tests/golden-path.spec.ts.
 * It just records every POST to /hook (headers PocketPress signs — see
 * internal/webhook/worker.go — plus the exact raw body, since HMAC
 * verification is byte-sensitive); the spec itself verifies the signature
 * with node:crypto (support/signature.ts) once it knows the webhook's
 * one-time secret, captured mid-test from the /settings/webhooks reveal
 * panel.
 */
export function createWebhookReceiver(host: string, port: number): Promise<WebhookReceiver> {
  const deliveries: RecordedDelivery[] = [];

  const server = http.createServer((req, res) => {
    const chunks: Buffer[] = [];
    req.on('data', (c: Buffer) => chunks.push(c));
    req.on('end', () => {
      const rawBody = Buffer.concat(chunks).toString('utf8');
      let payload: unknown = null;
      try {
        payload = rawBody ? JSON.parse(rawBody) : null;
      } catch {
        payload = null;
      }
      deliveries.push({
        receivedAt: new Date().toISOString(),
        event: headerValue(req.headers['x-pocketpress-event']),
        deliveryId: headerValue(req.headers['x-pocketpress-delivery']),
        timestamp: headerValue(req.headers['x-pocketpress-timestamp']),
        signature: headerValue(req.headers['x-pocketpress-signature']),
        rawBody,
        payload,
      });
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ ok: true }));
    });
  });

  return new Promise((resolve, reject) => {
    server.once('error', reject);
    server.listen(port, host, () => {
      const addr = server.address() as AddressInfo;
      console.log(`[e2e] webhook receiver listening on http://${host}:${addr.port}`);
      resolve({
        deliveries,
        close: () =>
          new Promise<void>((res, rej) => {
            server.close((err) => (err ? rej(err) : res()));
          }),
      });
    });
  });
}

function headerValue(v: string | string[] | undefined): string | null {
  if (v === undefined) return null;
  return Array.isArray(v) ? (v[0] ?? null) : v;
}

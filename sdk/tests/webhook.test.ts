import { createHmac } from "node:crypto";
import { describe, expect, it } from "vitest";
import { verifyWebhookSignature } from "../src/index.js";

function sign(secret: string, timestamp: string, body: string): string {
  const hex = createHmac("sha256", secret)
    .update(`${timestamp}.${body}`)
    .digest("hex");
  return `sha256=${hex}`;
}

describe("verifyWebhookSignature", () => {
  const secret = "whsec_test_secret";
  const body = '{"event":"post.published","id":"1"}';
  const timestamp = "1700000000";

  it("accepts a valid signature", () => {
    const signature = sign(secret, timestamp, body);
    expect(
      verifyWebhookSignature({
        signature,
        timestamp,
        body,
        secret,
        now: () => 1700000000 * 1000,
      }),
    ).toBe(true);
  });

  it("rejects a tampered signature", () => {
    const signature = sign(secret, timestamp, body);
    const bad =
      signature.slice(0, "sha256=".length) +
      (signature["sha256=".length] === "a" ? "b" : "a") +
      signature.slice("sha256=".length + 1);
    expect(
      verifyWebhookSignature({
        signature: bad,
        timestamp,
        body,
        secret,
        now: () => 1700000000 * 1000,
      }),
    ).toBe(false);
  });

  it("rejects a wrong body", () => {
    const signature = sign(secret, timestamp, body);
    expect(
      verifyWebhookSignature({
        signature,
        timestamp,
        body: body + " ",
        secret,
        now: () => 1700000000 * 1000,
      }),
    ).toBe(false);
  });

  it("rejects stale timestamps by default", () => {
    const signature = sign(secret, timestamp, body);
    expect(
      verifyWebhookSignature({
        signature,
        timestamp,
        body,
        secret,
        now: () => (1700000000 + 601) * 1000,
      }),
    ).toBe(false);
  });

  it("skips skew check when maxAgeSeconds is 0", () => {
    const signature = sign(secret, timestamp, body);
    expect(
      verifyWebhookSignature({
        signature,
        timestamp,
        body,
        secret,
        maxAgeSeconds: 0,
        now: () => (1700000000 + 10_000) * 1000,
      }),
    ).toBe(true);
  });

  it("rejects missing headers", () => {
    expect(
      verifyWebhookSignature({
        signature: null,
        timestamp,
        body,
        secret,
      }),
    ).toBe(false);
  });
});

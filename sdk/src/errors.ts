import type { PocketPressErrorBody } from "./types.js";

/**
 * Error thrown for non-success PocketPress API responses.
 * `code` is the machine-readable value from the error envelope when present
 * (e.g. `post_not_found`, `api_disabled`, `invalid_preview_token`).
 */
export class PocketPressError extends Error {
  readonly status: number;
  readonly code: string;
  readonly details: unknown;

  constructor(
    message: string,
    options: {
      status: number;
      code?: string;
      details?: unknown;
      cause?: unknown;
    },
  ) {
    super(message, options.cause !== undefined ? { cause: options.cause } : undefined);
    this.name = "PocketPressError";
    this.status = options.status;
    this.code = options.code ?? "unknown_error";
    this.details = options.details ?? null;
  }

  static fromEnvelope(
    status: number,
    body: { error?: PocketPressErrorBody } | null,
    fallbackMessage: string,
  ): PocketPressError {
    const err = body?.error;
    const options: {
      status: number;
      code?: string;
      details?: unknown;
    } = { status };
    if (err?.code !== undefined) {
      options.code = err.code;
    }
    options.details = err?.details ?? null;
    return new PocketPressError(err?.message ?? fallbackMessage, options);
  }
}

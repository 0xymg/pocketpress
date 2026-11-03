import { within } from '@testing-library/dom';
import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

import WebhookDeliveries from './WebhookDeliveries.svelte';

function jsonResponse(status: number, ok: boolean, body: unknown) {
	return { ok, status, text: async () => JSON.stringify(body) };
}

const DELIVERY_PENDING = {
	id: 'del-1',
	event: 'post.published',
	event_id: 'evt-1',
	status: 'pending' as const,
	attempt: 1,
	response_code: null,
	response_body: null,
	next_attempt_at: '2026-08-02T12:05:00Z',
	delivered_at: null,
	created_at: '2026-08-02T12:00:00Z'
};

const DELIVERY_SUCCESS = {
	id: 'del-2',
	event: 'post.updated',
	event_id: 'evt-2',
	status: 'success' as const,
	attempt: 1,
	response_code: 200,
	response_body: 'OK',
	next_attempt_at: null,
	delivered_at: '2026-08-02T12:00:05Z',
	created_at: '2026-08-02T12:00:00Z'
};

const DELIVERY_FAILED = {
	id: 'del-3',
	event: 'post.deleted',
	event_id: 'evt-3',
	status: 'failed' as const,
	attempt: 5,
	response_code: 500,
	response_body: 'Internal Server Error and some more text truncated by the server at some length',
	next_attempt_at: null,
	delivered_at: null,
	created_at: '2026-08-02T12:00:00Z'
};

function defaultDeliveriesResponse() {
	return {
		deliveries: [DELIVERY_PENDING, DELIVERY_SUCCESS, DELIVERY_FAILED],
		meta: { page: 1, per_page: 20, total_items: 3, total_pages: 1 }
	};
}

/** Routes every fetch call by method + whether the URL targets /retry or /deliveries. */
function routedFetch(overrides: {
	list?: (url: string) => ReturnType<typeof jsonResponse>;
	retry?: () => ReturnType<typeof jsonResponse>;
}) {
	return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === 'string' ? input : input.toString();
		const method = (init?.method ?? 'GET').toUpperCase();
		if (method === 'POST' && url.includes('/retry')) {
			return (overrides.retry ?? (() => jsonResponse(202, true, null)))();
		}
		if (method === 'GET' && url.includes('/deliveries')) {
			return (overrides.list ?? (() => jsonResponse(200, true, defaultDeliveriesResponse())))(url);
		}
		throw new Error(`Unhandled request in test: ${method} ${url}`);
	});
}

describe('WebhookDeliveries', () => {
	afterEach(() => {
		vi.unstubAllGlobals();
	});

	it('renders each delivery with a status badge (amber/teal/red), attempt and response code', async () => {
		vi.stubGlobal('fetch', routedFetch({}));

		render(WebhookDeliveries, { props: { webhookId: 'wh-1' } });

		await waitFor(() => {
			expect(screen.getByText('post.published')).toBeInTheDocument();
		});
		expect(screen.getByText('post.updated')).toBeInTheDocument();
		expect(screen.getByText('post.deleted')).toBeInTheDocument();

		expect(screen.getByText('pending').className).toContain('border-amber-300');
		expect(screen.getByText('success').className).toContain('border-teal-300');
		expect(screen.getByText('failed').className).toContain('border-red-300');

		expect(screen.getByText('200')).toBeInTheDocument();
		expect(screen.getByText('500')).toBeInTheDocument();
	});

	it('shows Retry only on the failed row, and calls the retry endpoint for that delivery', async () => {
		const fetchMock = routedFetch({});
		vi.stubGlobal('fetch', fetchMock);

		render(WebhookDeliveries, { props: { webhookId: 'wh-1' } });

		await waitFor(() => {
			expect(screen.getByText('post.deleted')).toBeInTheDocument();
		});

		const retryButtons = screen.getAllByRole('button', { name: 'Retry' });
		expect(retryButtons).toHaveLength(1);

		await fireEvent.click(retryButtons[0]);

		await waitFor(() => {
			const calledRetry = fetchMock.mock.calls.some(
				([reqUrl, init]) =>
					String(reqUrl).includes('/deliveries/del-3/retry') &&
					(init as RequestInit | undefined)?.method === 'POST'
			);
			expect(calledRetry).toBe(true);
		});
	});

	it('expands the truncated response body for the failed row on demand, with a truncation note', async () => {
		vi.stubGlobal('fetch', routedFetch({}));

		render(WebhookDeliveries, { props: { webhookId: 'wh-1' } });

		await waitFor(() => {
			expect(screen.getByText('post.deleted')).toBeInTheDocument();
		});

		expect(screen.queryByText(/Internal Server Error/)).not.toBeInTheDocument();

		const failedRow = screen.getByText('post.deleted').closest('tr') as HTMLElement;
		await fireEvent.click(within(failedRow).getByRole('button', { name: 'View response' }));

		expect(screen.getByText(DELIVERY_FAILED.response_body)).toBeInTheDocument();
		expect(screen.getByText('Response body (truncated by the server):')).toBeInTheDocument();
	});

	it('paginates using the meta from the response', async () => {
		const fetchMock = routedFetch({
			list: (url) => {
				// Note: a naive `url.includes('page=2')` would also match the
				// "per_page=20" query param on the very first (page 1) request.
				if (/[?&]page=2(&|$)/.test(url)) {
					return jsonResponse(200, true, {
						deliveries: [DELIVERY_PENDING],
						meta: { page: 2, per_page: 20, total_items: 21, total_pages: 2 }
					});
				}
				return jsonResponse(200, true, {
					deliveries: [DELIVERY_SUCCESS],
					meta: { page: 1, per_page: 20, total_items: 21, total_pages: 2 }
				});
			}
		});
		vi.stubGlobal('fetch', fetchMock);

		render(WebhookDeliveries, { props: { webhookId: 'wh-1' } });

		await waitFor(() => {
			expect(screen.getByText(/Page 1 of 2/)).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Next' }));

		await waitFor(() => {
			const calledPage2 = fetchMock.mock.calls.some(([reqUrl]) => /[?&]page=2(&|$)/.test(String(reqUrl)));
			expect(calledPage2).toBe(true);
		});
	});

	it('re-fetches the current page when refreshToken changes', async () => {
		const fetchMock = routedFetch({});
		vi.stubGlobal('fetch', fetchMock);

		const { rerender } = render(WebhookDeliveries, { props: { webhookId: 'wh-1', refreshToken: 0 } });

		await waitFor(() => {
			expect(fetchMock).toHaveBeenCalledTimes(1);
		});

		await rerender({ webhookId: 'wh-1', refreshToken: 1 });

		await waitFor(() => {
			expect(fetchMock).toHaveBeenCalledTimes(2);
		});
	});
});

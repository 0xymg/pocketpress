import { fireEvent, render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';

import { auth } from '$lib/auth.svelte';
import Page from './+page.svelte';

const OWNER = {
	id: 'owner-1',
	name: 'Ada Lovelace',
	email: 'ada@example.com',
	role: 'owner' as const,
	active: true,
	last_login_at: null,
	created_at: '2024-01-01T00:00:00Z'
};

const EDITOR = {
	id: 'editor-1',
	name: 'Grace Hopper',
	email: 'grace@example.com',
	role: 'editor' as const,
	active: true,
	last_login_at: null,
	created_at: '2024-01-01T00:00:00Z'
};

const EMPTY_FILTERS = { category_ids: [] as string[], tag_ids: [] as string[] };

const WEBHOOK_A = {
	id: 'wh-1',
	name: 'Product website',
	url: 'https://product.example.com/api/pocketpress-webhook',
	events: ['post.published', 'post.updated'],
	filters: EMPTY_FILTERS,
	active: true,
	created_at: '2026-08-01T00:00:00Z'
};

const WEBHOOK_B = {
	id: 'wh-2',
	name: 'Staging',
	url: 'https://staging.example.com/webhook',
	events: ['post.deleted'],
	filters: EMPTY_FILTERS,
	active: false,
	created_at: '2026-08-01T00:00:00Z'
};

const EMPTY_DELIVERIES = {
	deliveries: [],
	meta: { page: 1, per_page: 20, total_items: 0, total_pages: 1 }
};

function jsonResponse(status: number, ok: boolean, body: unknown) {
	return { ok, status, text: async () => JSON.stringify(body) };
}

/**
 * Routes every fetch call for the webhooks page + its nested WebhookDeliveries
 * component. Order matters: the more specific suffixes (/rotate-secret,
 * /test, /retry, /deliveries) are checked before the generic collection
 * routes so a webhook id never gets misrouted to the plain list/create case.
 */
function routedFetch(overrides: {
	list?: () => ReturnType<typeof jsonResponse>;
	create?: () => ReturnType<typeof jsonResponse>;
	patch?: () => ReturnType<typeof jsonResponse>;
	rotate?: () => ReturnType<typeof jsonResponse>;
	del?: () => ReturnType<typeof jsonResponse>;
	test?: () => ReturnType<typeof jsonResponse>;
	deliveries?: () => ReturnType<typeof jsonResponse>;
	retry?: () => ReturnType<typeof jsonResponse>;
}) {
	return vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
		const url = typeof input === 'string' ? input : input.toString();
		const method = (init?.method ?? 'GET').toUpperCase();

		if (method === 'POST' && url.includes('/rotate-secret')) {
			return (overrides.rotate ?? (() => jsonResponse(200, true, { secret: 'whsec_rotated999' })))();
		}
		if (method === 'POST' && url.endsWith('/test')) {
			return (overrides.test ?? (() => jsonResponse(202, true, { delivery_id: 'del-test-1' })))();
		}
		if (method === 'POST' && url.includes('/deliveries/') && url.endsWith('/retry')) {
			return (overrides.retry ?? (() => jsonResponse(202, true, null)))();
		}
		if (method === 'GET' && url.includes('/deliveries')) {
			return (overrides.deliveries ?? (() => jsonResponse(200, true, EMPTY_DELIVERIES)))();
		}
		if (method === 'DELETE') {
			return (overrides.del ?? (() => jsonResponse(204, true, null)))();
		}
		if (method === 'PATCH') {
			return (overrides.patch ?? (() => jsonResponse(200, true, { webhook: WEBHOOK_A })))();
		}
		if (method === 'POST' && url.endsWith('/webhooks')) {
			return (
				overrides.create ?? (() => jsonResponse(201, true, { webhook: WEBHOOK_A, secret: 'whsec_created_abc' }))
			)();
		}
		if (method === 'GET' && url.includes('/webhooks')) {
			return (overrides.list ?? (() => jsonResponse(200, true, { webhooks: [WEBHOOK_A] })))();
		}
		if (method === 'GET' && url.includes('/categories')) {
			return jsonResponse(200, true, { categories: [] });
		}
		if (method === 'GET' && url.includes('/tags')) {
			return jsonResponse(200, true, { tags: [] });
		}
		throw new Error(`Unhandled request in test: ${method} ${url}`);
	});
}

describe('Webhooks settings page', () => {
	afterEach(() => {
		vi.unstubAllGlobals();
		vi.useRealTimers();
		auth.user = null;
	});

	it('renders the webhook list with name, url, event badges and active state for an owner', async () => {
		auth.user = OWNER;
		vi.stubGlobal(
			'fetch',
			routedFetch({ list: () => jsonResponse(200, true, { webhooks: [WEBHOOK_A, WEBHOOK_B] }) })
		);

		render(Page);

		await waitFor(() => {
			expect(screen.getByText('Product website')).toBeInTheDocument();
		});
		expect(screen.getByText('Staging')).toBeInTheDocument();
		expect(screen.getByText('https://product.example.com/api/pocketpress-webhook')).toBeInTheDocument();
		expect(screen.getByText('post.published')).toBeInTheDocument();
		expect(screen.getByText('post.updated')).toBeInTheDocument();
		expect(screen.getByText('post.deleted')).toBeInTheDocument();
		// Scoped to the status Badge — "Active" also appears as the "New webhook"
		// form's own active-checkbox label, so a bare getByText('Active') is ambiguous.
		expect(screen.getByText('Active', { selector: '[data-slot="badge"]' })).toBeInTheDocument();
		expect(screen.getByText('Paused', { selector: '[data-slot="badge"]' })).toBeInTheDocument();
	});

	it('blocks editors with a friendly no-access state and never calls the webhooks API', async () => {
		auth.user = EDITOR;
		const fetchMock = routedFetch({});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);

		expect(
			screen.getByText("You don't have access to this page. Ask an owner for access.")
		).toBeInTheDocument();
		expect(fetchMock).not.toHaveBeenCalled();
	});

	it('creates a webhook, reveals the secret exactly once, and clears it on dismiss', async () => {
		auth.user = OWNER;
		vi.stubGlobal(
			'fetch',
			routedFetch({
				list: () => jsonResponse(200, true, { webhooks: [] }),
				create: () => jsonResponse(201, true, { webhook: WEBHOOK_A, secret: 'whsec_created_abc' })
			})
		);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('No webhooks yet — add your first one below.')).toBeInTheDocument();
		});

		await fireEvent.input(screen.getByLabelText('Name'), { target: { value: 'Product website' } });
		await fireEvent.input(screen.getByLabelText('URL'), {
			target: { value: 'https://product.example.com/api/pocketpress-webhook' }
		});
		await fireEvent.click(screen.getByLabelText('Post published'));
		await fireEvent.click(screen.getByRole('button', { name: 'New webhook' }));

		await waitFor(() => {
			expect(screen.getByText('whsec_created_abc')).toBeInTheDocument();
		});
		expect(screen.getByText(/shown only once/)).toBeInTheDocument();

		await fireEvent.click(screen.getByRole('button', { name: "I've stored it — dismiss" }));

		expect(screen.queryByText('whsec_created_abc')).not.toBeInTheDocument();
	});

	it('rotating a secret warns first, then reveals the new secret exactly once', async () => {
		auth.user = OWNER;
		vi.stubGlobal(
			'fetch',
			routedFetch({
				list: () => jsonResponse(200, true, { webhooks: [WEBHOOK_A] }),
				rotate: () => jsonResponse(200, true, { secret: 'whsec_rotated999' })
			})
		);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Product website')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Rotate secret' }));
		expect(screen.getByText(/old secret stops working/)).toBeInTheDocument();

		await fireEvent.click(screen.getByRole('button', { name: 'Confirm rotate' }));

		await waitFor(() => {
			expect(screen.getByText('whsec_rotated999')).toBeInTheDocument();
		});
		expect(screen.getByText(/shown only once/)).toBeInTheDocument();
	});

	it('toggles active via PATCH and reflects the returned webhook', async () => {
		auth.user = OWNER;
		const fetchMock = routedFetch({
			list: () => jsonResponse(200, true, { webhooks: [WEBHOOK_A] }),
			patch: () => jsonResponse(200, true, { webhook: { ...WEBHOOK_A, active: false } })
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Product website')).toBeInTheDocument();
		});
		expect(screen.getByText('Active', { selector: '[data-slot="badge"]' })).toBeInTheDocument();

		await fireEvent.click(screen.getByRole('button', { name: 'Pause' }));

		await waitFor(() => {
			expect(screen.getByText('Paused', { selector: '[data-slot="badge"]' })).toBeInTheDocument();
		});

		const patchCall = fetchMock.mock.calls.find(
			([, init]) => (init as RequestInit | undefined)?.method === 'PATCH'
		);
		expect(patchCall).toBeDefined();
		const body = JSON.parse(String((patchCall?.[1] as RequestInit).body));
		expect(body).toEqual({ active: false });
	});

	it('renders per-field validation_failed errors from the create form', async () => {
		auth.user = OWNER;
		vi.stubGlobal(
			'fetch',
			routedFetch({
				list: () => jsonResponse(200, true, { webhooks: [] }),
				create: () =>
					jsonResponse(400, false, {
						error: {
							code: 'validation_failed',
							message: 'Validation failed',
							details: {
								name: 'Name is required.',
								url: 'URL must be http or https.',
								events: 'Select at least one event.'
							}
						}
					})
			})
		);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('No webhooks yet — add your first one below.')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'New webhook' }));

		await waitFor(() => {
			expect(screen.getByText('Name is required.')).toBeInTheDocument();
		});
		expect(screen.getByText('URL must be http or https.')).toBeInTheDocument();
		expect(screen.getByText('Select at least one event.')).toBeInTheDocument();
	});

	it('deletes a webhook after inline confirmation', async () => {
		auth.user = OWNER;
		const fetchMock = routedFetch({ list: () => jsonResponse(200, true, { webhooks: [WEBHOOK_A] }) });
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Product website')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Delete' }));
		await fireEvent.click(screen.getByRole('button', { name: 'Confirm delete' }));

		await waitFor(() => {
			const calledDelete = fetchMock.mock.calls.some(
				([, init]) => (init as RequestInit | undefined)?.method === 'DELETE'
			);
			expect(calledDelete).toBe(true);
		});
		await waitFor(() => {
			expect(screen.getByText('No webhooks yet — add your first one below.')).toBeInTheDocument();
		});
	});

	it('expands deliveries on demand and renders status/response/retry from the nested table', async () => {
		auth.user = OWNER;
		vi.stubGlobal(
			'fetch',
			routedFetch({
				list: () => jsonResponse(200, true, { webhooks: [WEBHOOK_A] }),
				deliveries: () =>
					jsonResponse(200, true, {
						deliveries: [
							{
								id: 'del-1',
								event: 'post.published',
								event_id: 'evt-1',
								status: 'failed',
								attempt: 5,
								response_code: 500,
								response_body: 'boom',
								next_attempt_at: null,
								delivered_at: null,
								created_at: '2026-08-01T00:00:00Z'
							}
						],
						meta: { page: 1, per_page: 20, total_items: 1, total_pages: 1 }
					})
			})
		);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Product website')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'View deliveries' }));

		await waitFor(() => {
			expect(screen.getByText('failed')).toBeInTheDocument();
		});
		expect(screen.getByText('500')).toBeInTheDocument();
		expect(screen.getByRole('button', { name: 'Retry' })).toBeInTheDocument();
	});

	it('sends a test delivery, shows a queued note, auto-expands deliveries, and refreshes again after ~2s', async () => {
		vi.useFakeTimers({ shouldAdvanceTime: true });
		auth.user = OWNER;
		const fetchMock = routedFetch({
			list: () => jsonResponse(200, true, { webhooks: [WEBHOOK_A] })
		});
		vi.stubGlobal('fetch', fetchMock);

		render(Page);
		await waitFor(() => {
			expect(screen.getByText('Product website')).toBeInTheDocument();
		});

		await fireEvent.click(screen.getByRole('button', { name: 'Send test' }));

		await waitFor(() => {
			expect(screen.getByText('Test delivery queued')).toBeInTheDocument();
		});

		const deliveriesCallsBefore = fetchMock.mock.calls.filter(([reqUrl]) =>
			String(reqUrl).includes('/deliveries')
		).length;
		expect(deliveriesCallsBefore).toBeGreaterThanOrEqual(1);

		await vi.advanceTimersByTimeAsync(2100);

		await waitFor(() => {
			const deliveriesCallsAfter = fetchMock.mock.calls.filter(([reqUrl]) =>
				String(reqUrl).includes('/deliveries')
			).length;
			expect(deliveriesCallsAfter).toBeGreaterThan(deliveriesCallsBefore);
		});

		vi.useRealTimers();
	});
});

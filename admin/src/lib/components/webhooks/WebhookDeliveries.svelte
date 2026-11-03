<script lang="ts" module>
	export interface WebhookDeliveriesProps {
		/** Which webhook's delivery history to show. */
		webhookId: string;
		/** Bump this (e.g. += 1) to force a re-fetch of the current page from outside — e.g. right after "Send test". */
		refreshToken?: number;
	}
</script>

<script lang="ts">
	/**
	 * Per-webhook delivery history (plan sections 8/9/13 Phase 7): fetched on
	 * demand when the parent expands a webhook's "View deliveries" panel.
	 * Table of event/status/attempt/response code/timestamps, an expandable
	 * (truncated) response body per row, a Retry button on failed rows, and
	 * pagination — same shape as MediaGrid: this component owns its own
	 * fetching, the parent only tells it which webhook and when to refresh.
	 */
	import { ApiError, listDeliveries, retryDelivery, type DeliveryListMeta, type WebhookDelivery } from '$lib/api';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { DELIVERY_STATUS_BADGE_CLASSES } from '$lib/delivery-status';

	const PER_PAGE = 20;

	let { webhookId, refreshToken = 0 }: WebhookDeliveriesProps = $props();

	type LoadState = 'pending' | 'success' | 'error';

	let deliveries = $state<WebhookDelivery[]>([]);
	let meta = $state<DeliveryListMeta>({ page: 1, per_page: PER_PAGE, total_items: 0, total_pages: 1 });
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');
	let page = $state(1);

	let expandedBody = $state<Record<string, boolean>>({});
	let retryBusy = $state<Record<string, boolean>>({});
	let retryError = $state<Record<string, string>>({});

	function describeError(err: unknown): string {
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Something went wrong.';
	}

	async function load() {
		loadState = 'pending';
		try {
			const result = await listDeliveries(webhookId, { page, per_page: PER_PAGE });
			deliveries = result.deliveries;
			meta = result.meta;
			loadState = 'success';
		} catch (err) {
			loadError = describeError(err);
			loadState = 'error';
		}
	}

	$effect(() => {
		void webhookId;
		void page;
		void refreshToken;
		load();
	});

	function formatTimestamp(value: string | null): string {
		return value ? new Date(value).toLocaleString() : '—';
	}

	async function handleRetry(delivery: WebhookDelivery) {
		retryError = { ...retryError, [delivery.id]: '' };
		retryBusy = { ...retryBusy, [delivery.id]: true };
		try {
			await retryDelivery(delivery.id);
			await load();
		} catch (err) {
			retryError = { ...retryError, [delivery.id]: describeError(err) };
		} finally {
			retryBusy = { ...retryBusy, [delivery.id]: false };
		}
	}
</script>

<div class="space-y-3 border-t border-stone-200 pt-3">
	{#if loadState === 'pending' && deliveries.length === 0}
		<p class="text-sm text-stone-400">Loading deliveries…</p>
	{:else if loadState === 'error'}
		<p class="text-sm text-destructive">{loadError}</p>
	{:else if deliveries.length === 0}
		<p class="text-sm text-stone-400">No deliveries yet.</p>
	{:else}
		<div class="overflow-x-auto">
			<table class="w-full text-left text-sm">
				<thead>
					<tr class="border-b border-stone-200 text-stone-500">
						<th class="py-2 pr-4 font-medium">Event</th>
						<th class="py-2 pr-4 font-medium">Status</th>
						<th class="py-2 pr-4 font-medium">Attempt</th>
						<th class="py-2 pr-4 font-medium">Response</th>
						<th class="py-2 pr-4 font-medium">Delivered</th>
						<th class="py-2 pr-4 font-medium">Next attempt</th>
						<th class="py-2 pr-4 font-medium">Actions</th>
					</tr>
				</thead>
				<tbody>
					{#each deliveries as delivery (delivery.id)}
						<tr class="border-b border-stone-100 align-top">
							<td class="py-2 pr-4 font-mono text-xs text-stone-700">{delivery.event}</td>
							<td class="py-2 pr-4">
								<Badge variant="outline" class={DELIVERY_STATUS_BADGE_CLASSES[delivery.status]}>
									{delivery.status}
								</Badge>
							</td>
							<td class="py-2 pr-4 text-stone-600">{delivery.attempt}</td>
							<td class="py-2 pr-4 text-stone-600">{delivery.response_code ?? '—'}</td>
							<td class="py-2 pr-4 text-stone-500">{formatTimestamp(delivery.delivered_at)}</td>
							<td class="py-2 pr-4 text-stone-500">{formatTimestamp(delivery.next_attempt_at)}</td>
							<td class="py-2 pr-4">
								<div class="flex flex-col items-start gap-1">
									{#if delivery.response_body}
										<Button
											type="button"
											variant="outline"
											size="sm"
											onclick={() =>
												(expandedBody = {
													...expandedBody,
													[delivery.id]: !expandedBody[delivery.id]
												})}
										>
											{expandedBody[delivery.id] ? 'Hide response' : 'View response'}
										</Button>
									{/if}
									{#if delivery.status === 'failed'}
										<Button
											type="button"
											size="sm"
											disabled={retryBusy[delivery.id]}
											onclick={() => handleRetry(delivery)}
										>
											{retryBusy[delivery.id] ? 'Retrying…' : 'Retry'}
										</Button>
									{/if}
									{#if retryError[delivery.id]}
										<p class="text-xs text-destructive">{retryError[delivery.id]}</p>
									{/if}
								</div>
							</td>
						</tr>
						{#if expandedBody[delivery.id] && delivery.response_body}
							<tr class="border-b border-stone-100">
								<td colspan="7" class="py-2 pr-4">
									<p class="mb-1 text-xs text-stone-400">Response body (truncated by the server):</p>
									<pre
										class="max-h-48 overflow-auto rounded-md bg-stone-900 p-3 text-xs whitespace-pre-wrap text-stone-100">{delivery.response_body}</pre>
								</td>
							</tr>
						{/if}
					{/each}
				</tbody>
			</table>
		</div>

		{#if meta.total_pages > 1}
			<div class="flex items-center justify-between">
				<p class="text-xs text-stone-500">
					Page {meta.page} of {meta.total_pages} &middot; {meta.total_items} deliveries
				</p>
				<div class="flex gap-2">
					<Button
						type="button"
						variant="outline"
						size="sm"
						disabled={meta.page <= 1}
						onclick={() => (page = page - 1)}
					>
						Previous
					</Button>
					<Button
						type="button"
						variant="outline"
						size="sm"
						disabled={meta.page >= meta.total_pages}
						onclick={() => (page = page + 1)}
					>
						Next
					</Button>
				</div>
			</div>
		{/if}
	{/if}
</div>

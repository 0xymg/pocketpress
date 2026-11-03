<script lang="ts">
	/**
	 * Webhook management (Phase 7 — plan sections 8/9/13, route
	 * /settings/webhooks). Owner-only, same client-side guard pattern as
	 * /settings/general and /settings/users: the server enforces owner-only
	 * access on every /api/admin/v1/webhooks* call, this page mirrors that so
	 * editors get a friendly "no access" state instead of failed requests.
	 *
	 * The webhook secret is only ever returned by the create and
	 * rotate-secret endpoints (frozen contract) — `secretPanel` is the single
	 * place it's held in memory, and it's cleared as soon as the user
	 * dismisses the reveal panel. It is never merged into the `webhooks` list.
	 */
	import { onDestroy } from 'svelte';
	import * as Card from '$lib/components/ui/card/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import WebhookDeliveries from '$lib/components/webhooks/WebhookDeliveries.svelte';
	import { auth } from '$lib/auth.svelte';
	import {
		ApiError,
		createWebhook,
		deleteWebhook,
		listWebhooks,
		rotateWebhookSecret,
		testWebhook,
		updateWebhook,
		type Webhook,
		type WebhookEvent
	} from '$lib/api';

	type LoadState = 'pending' | 'success' | 'error';

	const isOwner = $derived(auth.user?.role === 'owner');

	const EVENT_OPTIONS: { value: WebhookEvent; label: string; description: string }[] = [
		{
			value: 'post.published',
			label: 'Post published',
			description: 'A post is published, including via a scheduled publish.'
		},
		{
			value: 'post.updated',
			label: 'Post updated',
			description: 'An already-published post is edited.'
		},
		{
			value: 'post.unpublished',
			label: 'Post unpublished',
			description: 'A published post is unpublished.'
		},
		{
			value: 'post.deleted',
			label: 'Post deleted',
			description: 'A post is deleted.'
		}
	];

	let webhooks = $state<Webhook[]>([]);
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	// --- create form ---
	let newName = $state('');
	let newUrl = $state('');
	let newEvents = $state<WebhookEvent[]>([]);
	let newActive = $state(true);
	let creating = $state(false);
	let createError = $state('');
	let createFieldErrors = $state<Record<string, string>>({});

	// --- inline edit (one row at a time) ---
	let editingId = $state<string | null>(null);
	let editName = $state('');
	let editUrl = $state('');
	let editEvents = $state<WebhookEvent[]>([]);
	let editActive = $state(true);
	let editFieldErrors = $state<Record<string, string>>({});

	// --- per-row transient state ---
	let rowBusy = $state<Record<string, boolean>>({});
	let rowError = $state<Record<string, string>>({});
	let confirmDeleteId = $state<string | null>(null);
	let confirmRotateId = $state<string | null>(null);
	let testBusy = $state<Record<string, boolean>>({});
	let testMessage = $state<Record<string, string>>({});
	let testError = $state<Record<string, string>>({});
	let expandedDeliveries = $state<Record<string, boolean>>({});
	let deliveriesRefreshToken = $state<Record<string, number>>({});

	/** The secret is shown only once — right after create or rotate-secret. Not part of `webhooks`. */
	let secretPanel = $state<{ webhookId: string; webhookName: string; secret: string; mode: 'created' | 'rotated' } | null>(
		null
	);
	let secretCopyFeedback = $state('');

	// setTimeout ids for the "refresh deliveries once more ~2s after a test
	// send" behavior — plain object (not reactive state), cleared on destroy.
	const testTimers: Record<string, ReturnType<typeof setTimeout>> = {};
	onDestroy(() => {
		for (const timer of Object.values(testTimers)) clearTimeout(timer);
	});

	function describeError(err: unknown): string {
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Something went wrong.';
	}

	async function loadWebhooks() {
		loadState = 'pending';
		try {
			const { webhooks: fetched } = await listWebhooks();
			webhooks = fetched;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load webhooks.';
			loadState = 'error';
		}
	}

	$effect(() => {
		if (isOwner) loadWebhooks();
	});

	function toggleNewEvent(value: WebhookEvent, checked: boolean) {
		newEvents = checked ? [...newEvents, value] : newEvents.filter((e) => e !== value);
	}

	function toggleEditEvent(value: WebhookEvent, checked: boolean) {
		editEvents = checked ? [...editEvents, value] : editEvents.filter((e) => e !== value);
	}

	async function handleCreate(event: SubmitEvent) {
		event.preventDefault();
		createError = '';
		createFieldErrors = {};
		creating = true;
		try {
			const { webhook, secret } = await createWebhook({
				name: newName,
				url: newUrl,
				events: newEvents,
				active: newActive
			});
			webhooks = [...webhooks, webhook];
			secretPanel = { webhookId: webhook.id, webhookName: webhook.name, secret, mode: 'created' };
			newName = '';
			newUrl = '';
			newEvents = [];
			newActive = true;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				createFieldErrors = err.details ?? {};
			}
			createError = describeError(err);
		} finally {
			creating = false;
		}
	}

	function startEdit(webhook: Webhook) {
		editingId = webhook.id;
		editName = webhook.name;
		editUrl = webhook.url;
		editEvents = [...webhook.events];
		editActive = webhook.active;
		editFieldErrors = {};
		rowError = { ...rowError, [webhook.id]: '' };
		confirmDeleteId = null;
		confirmRotateId = null;
	}

	function cancelEdit() {
		editingId = null;
		editFieldErrors = {};
	}

	async function saveEdit(id: string) {
		editFieldErrors = {};
		rowBusy = { ...rowBusy, [id]: true };
		try {
			const { webhook } = await updateWebhook(id, {
				name: editName,
				url: editUrl,
				events: editEvents,
				active: editActive
			});
			webhooks = webhooks.map((w) => (w.id === id ? webhook : w));
			editingId = null;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				editFieldErrors = err.details ?? {};
			}
			rowError = { ...rowError, [id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [id]: false };
		}
	}

	async function toggleActive(webhook: Webhook) {
		rowError = { ...rowError, [webhook.id]: '' };
		rowBusy = { ...rowBusy, [webhook.id]: true };
		try {
			const { webhook: updated } = await updateWebhook(webhook.id, { active: !webhook.active });
			webhooks = webhooks.map((w) => (w.id === updated.id ? updated : w));
		} catch (err) {
			rowError = { ...rowError, [webhook.id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [webhook.id]: false };
		}
	}

	async function handleDelete(id: string) {
		rowBusy = { ...rowBusy, [id]: true };
		rowError = { ...rowError, [id]: '' };
		try {
			await deleteWebhook(id);
			webhooks = webhooks.filter((w) => w.id !== id);
			confirmDeleteId = null;
		} catch (err) {
			rowError = { ...rowError, [id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [id]: false };
		}
	}

	async function handleRotate(webhook: Webhook) {
		rowBusy = { ...rowBusy, [webhook.id]: true };
		rowError = { ...rowError, [webhook.id]: '' };
		try {
			const { secret } = await rotateWebhookSecret(webhook.id);
			secretPanel = { webhookId: webhook.id, webhookName: webhook.name, secret, mode: 'rotated' };
			confirmRotateId = null;
		} catch (err) {
			rowError = { ...rowError, [webhook.id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [webhook.id]: false };
		}
	}

	function dismissSecretPanel() {
		secretPanel = null;
		secretCopyFeedback = '';
	}

	async function copySecret() {
		if (!secretPanel) return;
		try {
			await navigator.clipboard.writeText(secretPanel.secret);
			secretCopyFeedback = 'Copied!';
		} catch {
			secretCopyFeedback = 'Unable to copy — copy it manually.';
		}
	}

	function toggleDeliveries(id: string) {
		expandedDeliveries = { ...expandedDeliveries, [id]: !expandedDeliveries[id] };
	}

	function bumpDeliveries(id: string) {
		deliveriesRefreshToken = { ...deliveriesRefreshToken, [id]: (deliveriesRefreshToken[id] ?? 0) + 1 };
	}

	async function handleTest(webhook: Webhook) {
		testError = { ...testError, [webhook.id]: '' };
		testBusy = { ...testBusy, [webhook.id]: true };
		try {
			await testWebhook(webhook.id);
			testMessage = { ...testMessage, [webhook.id]: 'Test delivery queued' };
			expandedDeliveries = { ...expandedDeliveries, [webhook.id]: true };
			clearTimeout(testTimers[webhook.id]);
			// The delivery is processed asynchronously by the backend worker — give
			// it a couple of seconds, then refresh the (already open) deliveries
			// panel once so the new attempt shows up without a manual reload.
			testTimers[webhook.id] = setTimeout(() => {
				bumpDeliveries(webhook.id);
			}, 2000);
		} catch (err) {
			testError = { ...testError, [webhook.id]: describeError(err) };
		} finally {
			testBusy = { ...testBusy, [webhook.id]: false };
		}
	}
</script>

<header class="mb-8 pt-2">
	<h1 class="text-[32px] font-medium leading-9 text-stone-900">Webhooks</h1>
	<p class="mt-2 text-[13px] text-stone-500">Notify your product when posts change.</p>
</header>

{#if !isOwner}
			<Card.Root class="mt-8">
				<Card.Content class="py-10 text-center text-sm text-stone-500">
					You don't have access to this page. Ask an owner for access.
				</Card.Content>
			</Card.Root>
		{:else}
			{#if secretPanel}
				<Card.Root class="mt-8 border-amber-300 bg-amber-50">
					<Card.Header>
						<Card.Title class="text-sm font-medium text-amber-900">
							{secretPanel.mode === 'created' ? 'Webhook secret' : 'New webhook secret'} — {secretPanel.webhookName}
						</Card.Title>
					</Card.Header>
					<Card.Content class="space-y-3">
						<p class="text-sm font-semibold text-amber-900">
							This secret is shown only once — store it now. It will not be shown again.
						</p>
						<div class="flex items-center gap-2">
							<code
								class="flex-1 overflow-x-auto rounded-md border border-amber-300 bg-white px-3 py-2 font-mono text-sm text-stone-900"
							>
								{secretPanel.secret}
							</code>
							<Button type="button" variant="outline" size="sm" onclick={copySecret}>
								{secretCopyFeedback || 'Copy'}
							</Button>
						</div>
						<Button type="button" size="sm" onclick={dismissSecretPanel}>
							I've stored it — dismiss
						</Button>
					</Card.Content>
				</Card.Root>
			{/if}

			<div class="mt-8 space-y-4">
				{#if loadState === 'pending'}
					<Card.Root>
						<Card.Content class="py-10 text-center text-sm text-stone-400">
							Loading webhooks…
						</Card.Content>
					</Card.Root>
				{:else if loadState === 'error'}
					<Card.Root>
						<Card.Content class="py-10 text-center text-sm text-destructive">{loadError}</Card.Content>
					</Card.Root>
				{:else if webhooks.length === 0}
					<Card.Root>
						<Card.Content class="py-10 text-center text-sm text-stone-400">
							No webhooks yet — add your first one below.
						</Card.Content>
					</Card.Root>
				{:else}
					{#each webhooks as webhook (webhook.id)}
						<Card.Root>
							<Card.Content class="space-y-3 pt-6">
								{#if editingId === webhook.id}
									<div class="space-y-3">
										<div class="grid gap-3 sm:grid-cols-2">
											<div class="space-y-1.5">
												<Label for="edit-name-{webhook.id}">Name</Label>
												<Input
													id="edit-name-{webhook.id}"
													bind:value={editName}
													aria-invalid={!!editFieldErrors.name}
												/>
												{#if editFieldErrors.name}
													<p class="text-xs text-destructive">{editFieldErrors.name}</p>
												{/if}
											</div>
											<div class="space-y-1.5">
												<Label for="edit-url-{webhook.id}">URL</Label>
												<Input
													id="edit-url-{webhook.id}"
													type="url"
													bind:value={editUrl}
													aria-invalid={!!editFieldErrors.url}
												/>
												{#if editFieldErrors.url}
													<p class="text-xs text-destructive">{editFieldErrors.url}</p>
												{/if}
											</div>
										</div>

										<div class="space-y-1.5">
											<span class="text-sm font-medium text-stone-700">Events</span>
											<div class="grid gap-2 sm:grid-cols-2">
												{#each EVENT_OPTIONS as opt (opt.value)}
													<div class="flex items-start gap-2 rounded-md border border-stone-200 p-2">
														<input
															id="edit-event-{webhook.id}-{opt.value}"
															type="checkbox"
															class="mt-0.5"
															checked={editEvents.includes(opt.value)}
															onchange={(event) =>
																toggleEditEvent(opt.value, event.currentTarget.checked)}
														/>
														<div>
															<Label for="edit-event-{webhook.id}-{opt.value}" class="block">
																{opt.label}
															</Label>
															<p class="text-xs text-stone-500">{opt.description}</p>
														</div>
													</div>
												{/each}
											</div>
											{#if editFieldErrors.events}
												<p class="text-xs text-destructive">{editFieldErrors.events}</p>
											{/if}
										</div>

										<label class="flex items-center gap-2 text-sm text-stone-700">
											<input type="checkbox" bind:checked={editActive} />
											Active
										</label>

										{#if rowError[webhook.id]}
											<p class="text-sm text-destructive" role="alert">{rowError[webhook.id]}</p>
										{/if}

										<div class="flex gap-2">
											<Button
												size="sm"
												disabled={rowBusy[webhook.id]}
												onclick={() => saveEdit(webhook.id)}
											>
												{rowBusy[webhook.id] ? 'Saving…' : 'Save'}
											</Button>
											<Button
												variant="outline"
												size="sm"
												disabled={rowBusy[webhook.id]}
												onclick={cancelEdit}
											>
												Cancel
											</Button>
										</div>
									</div>
								{:else}
									<div class="flex flex-wrap items-start justify-between gap-4">
										<div class="min-w-0">
											<div class="flex flex-wrap items-center gap-2">
												<p class="font-medium text-stone-900">{webhook.name}</p>
												<Badge variant={webhook.active ? 'default' : 'secondary'}>
													{webhook.active ? 'Active' : 'Paused'}
												</Badge>
											</div>
											<p class="mt-0.5 truncate text-xs text-stone-500">{webhook.url}</p>
											<div class="mt-2 flex flex-wrap gap-1.5">
												{#each webhook.events as evt (evt)}
													<Badge variant="outline">{evt}</Badge>
												{/each}
											</div>
										</div>
										<div class="flex shrink-0 flex-wrap items-center gap-2">
											<Button
												variant="outline"
												size="sm"
												disabled={rowBusy[webhook.id]}
												onclick={() => toggleActive(webhook)}
											>
												{webhook.active ? 'Pause' : 'Activate'}
											</Button>
											<Button variant="outline" size="sm" onclick={() => startEdit(webhook)}>
												Edit
											</Button>
											<Button
												variant="outline"
												size="sm"
												disabled={testBusy[webhook.id]}
												onclick={() => handleTest(webhook)}
											>
												{testBusy[webhook.id] ? 'Sending…' : 'Send test'}
											</Button>
											<Button variant="outline" size="sm" onclick={() => toggleDeliveries(webhook.id)}>
												{expandedDeliveries[webhook.id] ? 'Hide deliveries' : 'View deliveries'}
											</Button>
											{#if confirmRotateId === webhook.id}
												<Button
													variant="destructive"
													size="sm"
													disabled={rowBusy[webhook.id]}
													onclick={() => handleRotate(webhook)}
												>
													{rowBusy[webhook.id] ? 'Rotating…' : 'Confirm rotate'}
												</Button>
												<Button
													variant="outline"
													size="sm"
													disabled={rowBusy[webhook.id]}
													onclick={() => (confirmRotateId = null)}
												>
													Cancel
												</Button>
											{:else}
												<Button
													variant="outline"
													size="sm"
													onclick={() => (confirmRotateId = webhook.id)}
												>
													Rotate secret
												</Button>
											{/if}
											{#if confirmDeleteId === webhook.id}
												<Button
													variant="destructive"
													size="sm"
													disabled={rowBusy[webhook.id]}
													onclick={() => handleDelete(webhook.id)}
												>
													Confirm delete
												</Button>
												<Button
													variant="outline"
													size="sm"
													disabled={rowBusy[webhook.id]}
													onclick={() => (confirmDeleteId = null)}
												>
													Cancel
												</Button>
											{:else}
												<Button
													variant="outline"
													size="sm"
													onclick={() => (confirmDeleteId = webhook.id)}
												>
													Delete
												</Button>
											{/if}
										</div>
									</div>

									{#if confirmRotateId === webhook.id}
										<p class="text-xs text-amber-700">
											Rotating replaces the current secret — the old secret stops working
											immediately, so update any deployed integration with the new one right away.
										</p>
									{/if}
									{#if testMessage[webhook.id]}
										<p class="text-xs text-teal-700">{testMessage[webhook.id]}</p>
									{/if}
									{#if testError[webhook.id]}
										<p class="text-xs text-destructive">{testError[webhook.id]}</p>
									{/if}
									{#if rowError[webhook.id]}
										<p class="text-xs text-destructive">{rowError[webhook.id]}</p>
									{/if}
								{/if}

								{#if expandedDeliveries[webhook.id]}
									<WebhookDeliveries
										webhookId={webhook.id}
										refreshToken={deliveriesRefreshToken[webhook.id] ?? 0}
									/>
								{/if}
							</Card.Content>
						</Card.Root>
					{/each}
				{/if}
			</div>

			<Card.Root class="mt-6">
				<Card.Header>
					<Card.Title class="text-sm font-medium text-stone-700">New webhook</Card.Title>
				</Card.Header>
				<Card.Content>
					<form class="space-y-4" onsubmit={handleCreate} novalidate>
						<div class="grid gap-4 sm:grid-cols-2">
							<div class="space-y-1.5">
								<Label for="new-webhook-name">Name</Label>
								<Input
									id="new-webhook-name"
									bind:value={newName}
									aria-invalid={!!createFieldErrors.name}
								/>
								{#if createFieldErrors.name}
									<p class="text-xs text-destructive">{createFieldErrors.name}</p>
								{/if}
							</div>
							<div class="space-y-1.5">
								<Label for="new-webhook-url">URL</Label>
								<Input
									id="new-webhook-url"
									type="url"
									placeholder="https://example.com/api/pocketpress-webhook"
									bind:value={newUrl}
									aria-invalid={!!createFieldErrors.url}
								/>
								{#if createFieldErrors.url}
									<p class="text-xs text-destructive">{createFieldErrors.url}</p>
								{/if}
							</div>
						</div>

						<div class="space-y-1.5">
							<span class="text-sm font-medium text-stone-700">Events</span>
							<div class="grid gap-2 sm:grid-cols-2">
								{#each EVENT_OPTIONS as opt (opt.value)}
									<div class="flex items-start gap-2 rounded-md border border-stone-200 p-2">
										<input
											id="new-event-{opt.value}"
											type="checkbox"
											class="mt-0.5"
											checked={newEvents.includes(opt.value)}
											onchange={(event) => toggleNewEvent(opt.value, event.currentTarget.checked)}
										/>
										<div>
											<Label for="new-event-{opt.value}" class="block">{opt.label}</Label>
											<p class="text-xs text-stone-500">{opt.description}</p>
										</div>
									</div>
								{/each}
							</div>
							{#if createFieldErrors.events}
								<p class="text-xs text-destructive">{createFieldErrors.events}</p>
							{/if}
						</div>

						<label class="flex items-center gap-2 text-sm text-stone-700">
							<input type="checkbox" bind:checked={newActive} />
							Active
						</label>

						{#if createError}
							<p class="text-sm text-destructive" role="alert">{createError}</p>
						{/if}

						<Button type="submit" disabled={creating}>
							{creating ? 'Creating…' : 'New webhook'}
						</Button>

						<details class="rounded-md border border-stone-200 bg-stone-50 p-3 text-stone-600">
							<summary class="cursor-pointer text-sm font-medium text-stone-700 select-none">
								Verifying signatures
							</summary>
							<div class="mt-3 space-y-2 text-xs">
								<p>Every delivery includes these headers:</p>
								<ul class="list-disc space-y-1 pl-5 font-mono">
									<li>X-PocketPress-Event</li>
									<li>X-PocketPress-Delivery</li>
									<li>X-PocketPress-Timestamp</li>
									<li>X-PocketPress-Signature: sha256=...</li>
								</ul>
								<p>Verify a delivery by computing:</p>
								<pre
									class="overflow-x-auto rounded bg-stone-900 p-2 text-[11px] text-stone-100">HMAC-SHA256(secret, timestamp + "." + raw_request_body)</pre>
								<p>
									then comparing the hex-encoded result to the value after <code>sha256=</code> in
									<code>X-PocketPress-Signature</code>, using a constant-time comparison.
								</p>
							</div>
						</details>
					</form>
				</Card.Content>
			</Card.Root>
	{/if}

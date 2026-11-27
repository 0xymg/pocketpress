<script lang="ts">
	/**
	 * API key management (v0.2 — plan §16). Owner-only. The raw key is shown
	 * exactly once after create (keyPanel); it is never merged into the list.
	 */
	import * as Card from '$lib/components/ui/card/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { auth } from '$lib/auth.svelte';
	import {
		ApiError,
		createAPIKey,
		deleteAPIKey,
		listAPIKeys,
		revokeAPIKey,
		type APIKey,
		type APIKeyScope
	} from '$lib/api';

	type LoadState = 'pending' | 'success' | 'error';

	const isOwner = $derived(auth.user?.role === 'owner');

	const SCOPE_OPTIONS: { value: APIKeyScope; label: string; description: string }[] = [
		{
			value: 'content:read',
			label: 'Content read',
			description: 'Access posts, categories, tags, feeds and sitemap when the public API is disabled.'
		},
		{
			value: 'preview:read',
			label: 'Preview read',
			description: 'Preview draft posts via Bearer auth (no short-lived preview token required).'
		}
	];

	let keys = $state<APIKey[]>([]);
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	let newName = $state('');
	let newScopes = $state<APIKeyScope[]>(['content:read']);
	let creating = $state(false);
	let createError = $state('');
	let createFieldErrors = $state<Record<string, string>>({});

	let rowBusy = $state<Record<string, boolean>>({});
	let rowError = $state<Record<string, string>>({});
	let confirmRevokeId = $state<string | null>(null);
	let confirmDeleteId = $state<string | null>(null);

	let keyPanel = $state<{ name: string; key: string } | null>(null);
	let keyCopyFeedback = $state('');

	function describeError(err: unknown): string {
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Something went wrong.';
	}

	async function loadKeys() {
		loadState = 'pending';
		try {
			const { api_keys } = await listAPIKeys();
			keys = api_keys;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load API keys.';
			loadState = 'error';
		}
	}

	$effect(() => {
		if (isOwner) loadKeys();
	});

	function toggleScope(value: APIKeyScope, checked: boolean) {
		newScopes = checked ? [...newScopes, value] : newScopes.filter((s) => s !== value);
	}

	async function handleCreate(event: SubmitEvent) {
		event.preventDefault();
		createError = '';
		createFieldErrors = {};
		creating = true;
		try {
			const { api_key, key } = await createAPIKey({ name: newName, scopes: newScopes });
			keys = [...keys, api_key];
			keyPanel = { name: api_key.name, key };
			newName = '';
			newScopes = ['content:read'];
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				createFieldErrors = err.details ?? {};
			}
			createError = describeError(err);
		} finally {
			creating = false;
		}
	}

	async function handleRevoke(id: string) {
		rowBusy = { ...rowBusy, [id]: true };
		rowError = { ...rowError, [id]: '' };
		try {
			const { api_key } = await revokeAPIKey(id);
			keys = keys.map((k) => (k.id === id ? api_key : k));
			confirmRevokeId = null;
		} catch (err) {
			rowError = { ...rowError, [id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [id]: false };
		}
	}

	async function handleDelete(id: string) {
		rowBusy = { ...rowBusy, [id]: true };
		rowError = { ...rowError, [id]: '' };
		try {
			await deleteAPIKey(id);
			keys = keys.filter((k) => k.id !== id);
			confirmDeleteId = null;
		} catch (err) {
			rowError = { ...rowError, [id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [id]: false };
		}
	}

	async function copyKey() {
		if (!keyPanel) return;
		try {
			await navigator.clipboard.writeText(keyPanel.key);
			keyCopyFeedback = 'Copied!';
		} catch {
			keyCopyFeedback = 'Unable to copy — copy it manually.';
		}
	}

	function dismissKeyPanel() {
		keyPanel = null;
		keyCopyFeedback = '';
	}

	function formatWhen(iso: string | null): string {
		if (!iso) return '—';
		try {
			return new Date(iso).toLocaleString();
		} catch {
			return iso;
		}
	}
</script>

{#if !isOwner}
	<div class="mx-auto max-w-2xl py-16 text-center">
		<h1 class="text-xl font-semibold">API keys</h1>
		<p class="mt-2 text-muted-foreground">Only the site owner can manage API keys.</p>
	</div>
{:else}
	<div class="mx-auto flex max-w-3xl flex-col gap-8 py-8">
		<header>
			<h1 class="text-2xl font-semibold tracking-tight">API keys</h1>
			<p class="mt-1 text-sm text-muted-foreground">
				Issue Bearer keys for private content API access when Public API is disabled. Raw keys are
				hashed at rest and shown only once.
			</p>
		</header>

		{#if keyPanel}
			<Card.Root class="border-amber-500/40 bg-amber-50/40 dark:bg-amber-950/20">
				<Card.Header>
					<Card.Title>Copy your API key</Card.Title>
					<Card.Description>
						This is the only time the full key for “{keyPanel.name}” will be shown.
					</Card.Description>
				</Card.Header>
				<Card.Content class="flex flex-col gap-3">
					<code class="break-all rounded-md bg-background px-3 py-2 text-sm">{keyPanel.key}</code>
					<div class="flex flex-wrap items-center gap-2">
						<Button type="button" size="sm" onclick={copyKey}>Copy key</Button>
						<Button type="button" size="sm" variant="ghost" onclick={dismissKeyPanel}>Done</Button>
						{#if keyCopyFeedback}
							<span class="text-sm text-muted-foreground">{keyCopyFeedback}</span>
						{/if}
					</div>
				</Card.Content>
			</Card.Root>
		{/if}

		<Card.Root>
			<Card.Header>
				<Card.Title>Create API key</Card.Title>
			</Card.Header>
			<Card.Content>
				<form class="flex flex-col gap-4" onsubmit={handleCreate}>
					<div class="flex flex-col gap-1.5">
						<Label for="api-key-name">Name</Label>
						<Input id="api-key-name" bind:value={newName} placeholder="CI reader" required />
						{#if createFieldErrors.name}
							<p class="text-sm text-destructive">{createFieldErrors.name}</p>
						{/if}
					</div>
					<fieldset class="flex flex-col gap-2">
						<legend class="text-sm font-medium">Scopes</legend>
						{#each SCOPE_OPTIONS as opt}
							<label class="flex items-start gap-2 text-sm">
								<input
									type="checkbox"
									class="mt-1"
									checked={newScopes.includes(opt.value)}
									onchange={(e) => toggleScope(opt.value, e.currentTarget.checked)}
								/>
								<span>
									<span class="font-medium">{opt.label}</span>
									<span class="block text-muted-foreground">{opt.description}</span>
								</span>
							</label>
						{/each}
						{#if createFieldErrors.scopes}
							<p class="text-sm text-destructive">{createFieldErrors.scopes}</p>
						{/if}
					</fieldset>
					{#if createError}
						<p class="text-sm text-destructive">{createError}</p>
					{/if}
					<Button type="submit" disabled={creating}>{creating ? 'Creating…' : 'Create key'}</Button>
				</form>
			</Card.Content>
		</Card.Root>

		<section class="flex flex-col gap-3">
			<h2 class="text-lg font-medium">Existing keys</h2>
			{#if loadState === 'pending'}
				<p class="text-sm text-muted-foreground">Loading…</p>
			{:else if loadState === 'error'}
				<p class="text-sm text-destructive">{loadError}</p>
			{:else if keys.length === 0}
				<p class="text-sm text-muted-foreground">No API keys yet — create one above.</p>
			{:else}
				<ul class="flex flex-col gap-3">
					{#each keys as key (key.id)}
						<li class="rounded-lg border p-4">
							<div class="flex flex-wrap items-start justify-between gap-3">
								<div>
									<div class="flex flex-wrap items-center gap-2">
										<span class="font-medium">{key.name}</span>
										<code class="text-xs text-muted-foreground">{key.prefix}…</code>
										{#if key.revoked_at}
											<Badge variant="secondary">Revoked</Badge>
										{/if}
									</div>
									<p class="mt-1 text-xs text-muted-foreground">
										Scopes: {key.scopes.join(', ') || '—'} · Created {formatWhen(key.created_at)} · Last
										used {formatWhen(key.last_used_at)}
										{#if key.expires_at}
											· Expires {formatWhen(key.expires_at)}
										{/if}
									</p>
								</div>
								<div class="flex flex-wrap gap-2">
									{#if !key.revoked_at}
										{#if confirmRevokeId === key.id}
											<Button
												type="button"
												size="sm"
												variant="destructive"
												disabled={rowBusy[key.id]}
												onclick={() => handleRevoke(key.id)}>Confirm revoke</Button
											>
											<Button type="button" size="sm" variant="ghost" onclick={() => (confirmRevokeId = null)}
												>Cancel</Button
											>
										{:else}
											<Button
												type="button"
												size="sm"
												variant="outline"
												onclick={() => {
													confirmRevokeId = key.id;
													confirmDeleteId = null;
												}}>Revoke</Button
											>
										{/if}
									{/if}
									{#if confirmDeleteId === key.id}
										<Button
											type="button"
											size="sm"
											variant="destructive"
											disabled={rowBusy[key.id]}
											onclick={() => handleDelete(key.id)}>Confirm delete</Button
										>
										<Button type="button" size="sm" variant="ghost" onclick={() => (confirmDeleteId = null)}
											>Cancel</Button
										>
									{:else}
										<Button
											type="button"
											size="sm"
											variant="ghost"
											onclick={() => {
												confirmDeleteId = key.id;
												confirmRevokeId = null;
											}}>Delete</Button
										>
									{/if}
								</div>
							</div>
							{#if rowError[key.id]}
								<p class="mt-2 text-sm text-destructive">{rowError[key.id]}</p>
							{/if}
						</li>
					{/each}
				</ul>
			{/if}
		</section>
	</div>
{/if}

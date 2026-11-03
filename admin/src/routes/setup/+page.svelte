<script lang="ts">
	/**
	 * First-run setup wizard (plan section 9). Only reachable while the
	 * instance is not yet initialized — src/routes/+layout.svelte's guard
	 * redirects here when GET /setup/status says {"initialized": false}, and
	 * redirects away from here once it flips to true (the server enforces
	 * this too; the UI just mirrors it so /setup doesn't dead-end).
	 */
	import * as Card from '$lib/components/ui/card/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { ApiError } from '$lib/api';
	import { buildTimezoneOptions, detectTimezone } from '$lib/timezones';

	const timezoneOptions = buildTimezoneOptions();

	let siteName = $state('');
	let siteUrl = $state('');
	let ownerName = $state('');
	let ownerEmail = $state('');
	let password = $state('');
	let timezone = $state(detectTimezone());

	let submitting = $state(false);
	let formError = $state('');
	let fieldErrors = $state<Record<string, string>>({});

	function validate(): boolean {
		const errors: Record<string, string> = {};
		if (!siteName.trim()) errors.site_name = 'Site name is required.';
		if (!siteUrl.trim()) errors.site_url = 'Site URL is required.';
		if (!ownerName.trim()) errors.owner_name = 'Owner name is required.';
		if (!ownerEmail.trim()) errors.owner_email = 'Owner email is required.';
		if (!password) errors.password = 'Password is required.';
		if (!timezone) errors.timezone = 'Timezone is required.';
		fieldErrors = errors;
		return Object.keys(errors).length === 0;
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		formError = '';

		if (!validate()) return;

		submitting = true;
		try {
			await auth.setup({
				site_name: siteName,
				site_url: siteUrl,
				owner_name: ownerName,
				owner_email: ownerEmail,
				password,
				timezone
			});
			// Plan section 9: after first-run setup, drop the owner straight into
			// writing their first post.
			await goto('/posts/new');
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				fieldErrors = err.details ?? {};
				formError = err.message;
			} else if (err instanceof ApiError && err.code === 'already_initialized') {
				formError = 'Setup has already been completed.';
			} else {
				formError = err instanceof Error ? err.message : 'Something went wrong. Please try again.';
			}
		} finally {
			submitting = false;
		}
	}
</script>

<div class="flex min-h-screen items-center justify-center bg-stone-50 px-4 py-10">
	<Card.Root class="w-full max-w-lg">
		<Card.Header>
			<h1 class="text-2xl font-semibold text-stone-900">Set up PocketPress</h1>
			<Card.Description>Create your site and owner account to get started.</Card.Description>
		</Card.Header>

		<Card.Content>
			<form class="space-y-4" onsubmit={handleSubmit} novalidate>
				<div class="space-y-1.5">
					<Label for="site-name">Site name</Label>
					<Input id="site-name" name="site_name" bind:value={siteName} aria-invalid={!!fieldErrors.site_name} />
					{#if fieldErrors.site_name}
						<p class="text-xs text-destructive">{fieldErrors.site_name}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label for="site-url">Site URL</Label>
					<Input
						id="site-url"
						name="site_url"
						type="url"
						placeholder="https://example.com"
						bind:value={siteUrl}
						aria-invalid={!!fieldErrors.site_url}
					/>
					{#if fieldErrors.site_url}
						<p class="text-xs text-destructive">{fieldErrors.site_url}</p>
					{/if}
				</div>

				<div class="grid gap-4 sm:grid-cols-2">
					<div class="space-y-1.5">
						<Label for="owner-name">Owner name</Label>
						<Input
							id="owner-name"
							name="owner_name"
							bind:value={ownerName}
							aria-invalid={!!fieldErrors.owner_name}
						/>
						{#if fieldErrors.owner_name}
							<p class="text-xs text-destructive">{fieldErrors.owner_name}</p>
						{/if}
					</div>

					<div class="space-y-1.5">
						<Label for="owner-email">Owner email</Label>
						<Input
							id="owner-email"
							name="owner_email"
							type="email"
							autocomplete="email"
							bind:value={ownerEmail}
							aria-invalid={!!fieldErrors.owner_email}
						/>
						{#if fieldErrors.owner_email}
							<p class="text-xs text-destructive">{fieldErrors.owner_email}</p>
						{/if}
					</div>
				</div>

				<div class="space-y-1.5">
					<Label for="password">Password</Label>
					<Input
						id="password"
						name="password"
						type="password"
						autocomplete="new-password"
						bind:value={password}
						aria-invalid={!!fieldErrors.password}
					/>
					{#if fieldErrors.password}
						<p class="text-xs text-destructive">{fieldErrors.password}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label for="timezone">Timezone</Label>
					<select
						id="timezone"
						name="timezone"
						class="border-input h-9 w-full rounded-md border bg-transparent px-3 py-1 text-sm shadow-xs"
						bind:value={timezone}
						aria-invalid={!!fieldErrors.timezone}
					>
						{#each timezoneOptions as zone (zone)}
							<option value={zone}>{zone}</option>
						{/each}
					</select>
					{#if fieldErrors.timezone}
						<p class="text-xs text-destructive">{fieldErrors.timezone}</p>
					{/if}
				</div>

				{#if formError}
					<p class="text-sm text-destructive" role="alert">{formError}</p>
				{/if}

				<Button type="submit" variant="primary" class="w-full" disabled={submitting}>
					{submitting ? 'Setting up…' : 'Create site'}
				</Button>
			</form>
		</Card.Content>
	</Card.Root>
</div>

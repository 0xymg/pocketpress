<script lang="ts">
	/**
	 * General settings (Phase 6 — plan section 9 route: /settings/general).
	 * Owner-only, same pattern as /settings/users: the server enforces it on
	 * every /api/admin/v1/settings call, this page mirrors that client-side
	 * so editors get a friendly "no access" state instead of failed requests.
	 *
	 * PATCH is partial (frozen contract) — handleSubmit diffs the form against
	 * the last-loaded snapshot and sends only the fields that actually
	 * changed. default_author and webhook_user_agent aren't in this form at
	 * all (Phase 7 / rare), so they're never touched by these PATCHes and the
	 * server keeps whatever they were.
	 */
	import * as Card from '$lib/components/ui/card/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { auth } from '$lib/auth.svelte';
	import { ApiError, getSettings, updateSettings, type Settings, type SettingsPayload } from '$lib/api';
	import { buildTimezoneOptions } from '$lib/timezones';

	type LoadState = 'pending' | 'success' | 'error';

	const isOwner = $derived(auth.user?.role === 'owner');
	const timezoneOptions = buildTimezoneOptions();

	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	/** The last-loaded/saved server snapshot — the diff base for PATCH and the dirty check. */
	let baseline = $state<Settings | null>(null);

	let siteName = $state('');
	let siteUrl = $state('');
	let adminTitle = $state('');
	let locale = $state('');
	let availableLocalesText = $state('');
	let timezone = $state('');
	let previewUrlTemplate = $state('');
	let corsOriginsText = $state('');
	let publicApiEnabled = $state(false);

	let saving = $state(false);
	let saveError = $state('');
	let fieldErrors = $state<Record<string, string>>({});
	let savedAt = $state<Date | null>(null);

	function applySettings(settings: Settings) {
		baseline = settings;
		siteName = settings.site_name;
		siteUrl = settings.site_url;
		adminTitle = settings.admin_title;
		locale = settings.locale;
		availableLocalesText = (settings.available_locales ?? []).join('\n');
		timezone = settings.timezone;
		previewUrlTemplate = settings.preview_url_template ?? '';
		corsOriginsText = settings.cors_origins.join('\n');
		publicApiEnabled = settings.public_api_enabled;
	}

	async function loadSettings() {
		loadState = 'pending';
		try {
			const { settings } = await getSettings();
			applySettings(settings);
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load settings.';
			loadState = 'error';
		}
	}

	$effect(() => {
		if (isOwner) loadSettings();
	});

	function parseCorsOrigins(text: string): string[] {
		return text
			.split('\n')
			.map((line) => line.trim())
			.filter((line) => line.length > 0);
	}

	function sameOrigins(a: string[], b: string[]): boolean {
		return a.length === b.length && a.every((value, i) => value === b[i]);
	}

	/** Diffs the live form against `baseline`, returning only the fields that changed — PATCH is partial, so untouched fields (including default_author/webhook_user_agent, which aren't even in this form) are never sent. */
	function buildChangedPayload(): SettingsPayload {
		if (!baseline) return {};
		const payload: SettingsPayload = {};
		if (siteName !== baseline.site_name) payload.site_name = siteName;
		if (siteUrl !== baseline.site_url) payload.site_url = siteUrl;
		if (adminTitle !== baseline.admin_title) payload.admin_title = adminTitle;
		if (locale !== baseline.locale) payload.locale = locale;
		if (timezone !== baseline.timezone) payload.timezone = timezone;

		const locales = parseCorsOrigins(availableLocalesText);
		if (!sameOrigins(locales, baseline.available_locales ?? [])) {
			payload.available_locales = locales;
		}

		const normalizedTemplate = previewUrlTemplate.trim() === '' ? null : previewUrlTemplate;
		if (normalizedTemplate !== baseline.preview_url_template) {
			payload.preview_url_template = normalizedTemplate;
		}

		const origins = parseCorsOrigins(corsOriginsText);
		if (!sameOrigins(origins, baseline.cors_origins)) payload.cors_origins = origins;

		if (publicApiEnabled !== baseline.public_api_enabled) {
			payload.public_api_enabled = publicApiEnabled;
		}

		return payload;
	}

	function formatSavedAt(date: Date): string {
		const hh = String(date.getHours()).padStart(2, '0');
		const mm = String(date.getMinutes()).padStart(2, '0');
		return `Saved ${hh}:${mm}`;
	}

	async function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		saveError = '';
		fieldErrors = {};

		const payload = buildChangedPayload();
		if (Object.keys(payload).length === 0) {
			savedAt = new Date();
			return;
		}

		saving = true;
		try {
			const { settings } = await updateSettings(payload);
			applySettings(settings);
			savedAt = new Date();
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				fieldErrors = err.details ?? {};
				saveError = err.message;
			} else if (err instanceof ApiError) {
				saveError = err.message;
			} else {
				saveError = 'Unable to save settings.';
			}
		} finally {
			saving = false;
		}
	}
</script>

<header class="mb-8 pt-2">
	<h1 class="text-[32px] font-medium leading-9 text-stone-900">General settings</h1>
	<p class="mt-2 text-[13px] text-stone-500">Site identity, timezone and preview/API configuration.</p>
</header>

{#if !isOwner}
			<Card.Root class="mt-8">
				<Card.Content class="py-10 text-center text-sm text-stone-500">
					You don't have access to this page. Ask an owner for access.
				</Card.Content>
			</Card.Root>
		{:else if loadState === 'pending'}
			<Card.Root class="mt-8">
				<Card.Content class="py-10 text-center text-sm text-stone-400">
					Loading settings…
				</Card.Content>
			</Card.Root>
		{:else if loadState === 'error'}
			<Card.Root class="mt-8">
				<Card.Content class="py-10 text-center text-sm text-destructive">{loadError}</Card.Content>
			</Card.Root>
		{:else}
			<Card.Root class="mt-8">
				<Card.Content class="pt-6">
					<form class="space-y-5" onsubmit={handleSubmit} novalidate>
						<div class="grid gap-4 sm:grid-cols-2">
							<div class="space-y-1.5">
								<Label for="settings-site-name">Site name</Label>
								<Input
									id="settings-site-name"
									bind:value={siteName}
									aria-invalid={!!fieldErrors.site_name}
								/>
								{#if fieldErrors.site_name}
									<p class="text-xs text-destructive">{fieldErrors.site_name}</p>
								{/if}
							</div>

							<div class="space-y-1.5">
								<Label for="settings-site-url">Site URL</Label>
								<Input
									id="settings-site-url"
									type="url"
									placeholder="https://example.com"
									bind:value={siteUrl}
									aria-invalid={!!fieldErrors.site_url}
								/>
								{#if fieldErrors.site_url}
									<p class="text-xs text-destructive">{fieldErrors.site_url}</p>
								{/if}
							</div>

							<div class="space-y-1.5">
								<Label for="settings-admin-title">Admin title</Label>
								<Input
									id="settings-admin-title"
									bind:value={adminTitle}
									aria-invalid={!!fieldErrors.admin_title}
								/>
								{#if fieldErrors.admin_title}
									<p class="text-xs text-destructive">{fieldErrors.admin_title}</p>
								{/if}
							</div>

							<div class="space-y-1.5">
								<Label for="settings-locale">Default locale</Label>
								<Input
									id="settings-locale"
									class="max-w-[10rem]"
									placeholder="en"
									bind:value={locale}
									aria-invalid={!!fieldErrors.locale}
								/>
								{#if fieldErrors.locale}
									<p class="text-xs text-destructive">{fieldErrors.locale}</p>
								{/if}
							</div>

							<div class="space-y-1.5">
								<Label for="settings-available-locales">Available locales</Label>
								<Textarea
									id="settings-available-locales"
									rows={3}
									placeholder={"en\ntr\nde"}
									bind:value={availableLocalesText}
									aria-invalid={!!fieldErrors.available_locales}
								/>
								<p class="text-xs text-stone-500">One BCP 47 language tag per line (e.g. en, tr, pt-br).</p>
								{#if fieldErrors.available_locales}
									<p class="text-xs text-destructive">{fieldErrors.available_locales}</p>
								{/if}
							</div>

							<div class="space-y-1.5">
								<Label for="settings-timezone">Timezone</Label>
								<select
									id="settings-timezone"
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
						</div>

						<div class="space-y-1.5">
							<Label for="settings-preview-url-template">Preview URL template</Label>
							<Input
								id="settings-preview-url-template"
								placeholder="https://example.com/api/preview?token={'{token}'}&slug={'{slug}'}"
								bind:value={previewUrlTemplate}
								aria-invalid={!!fieldErrors.preview_url_template}
							/>
							<p class="text-xs text-stone-500">
								The <code>{'{token}'}</code> and <code>{'{slug}'}</code> placeholders are substituted
								by the server when a preview link is generated. Example:
								<code>https://example.com/api/preview?token={'{token}'}&slug={'{slug}'}</code>. Leave
								blank to disable Preview (editors will see a hint pointing here instead).
							</p>
							{#if fieldErrors.preview_url_template}
								<p class="text-xs text-destructive">{fieldErrors.preview_url_template}</p>
							{/if}
						</div>

						<div class="space-y-1.5">
							<Label for="settings-cors-origins">CORS origins</Label>
							<Textarea
								id="settings-cors-origins"
								class="min-h-24 font-mono text-xs"
								placeholder={'https://example.com\nhttps://www.example.com'}
								bind:value={corsOriginsText}
								aria-invalid={!!fieldErrors.cors_origins}
							/>
							<p class="text-xs text-stone-500">One origin per line.</p>
							{#if fieldErrors.cors_origins}
								<p class="text-xs text-destructive">{fieldErrors.cors_origins}</p>
							{/if}
						</div>

						<div class="space-y-1.5">
							<label class="flex items-center gap-2 text-sm text-stone-700">
								<input
									id="settings-public-api-enabled"
									type="checkbox"
									bind:checked={publicApiEnabled}
								/>
								Public API enabled
							</label>
							{#if fieldErrors.public_api_enabled}
								<p class="text-xs text-destructive">{fieldErrors.public_api_enabled}</p>
							{/if}
						</div>

						{#if saveError}
							<p class="text-sm text-destructive" role="alert">{saveError}</p>
						{/if}

						<div class="flex items-center gap-3">
							<Button type="submit" disabled={saving}>
								{saving ? 'Saving…' : 'Save'}
							</Button>
							{#if savedAt}
								<span class="text-xs text-stone-400">{formatSavedAt(savedAt)}</span>
							{/if}
						</div>
					</form>
				</Card.Content>
			</Card.Root>
	{/if}

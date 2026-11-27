<script lang="ts">
	/**
	 * Self-service author profile (v0.3). Any signed-in user can edit their
	 * public author fields; role/active stay on the owner-only Users page.
	 */
	import * as Card from '$lib/components/ui/card/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { auth } from '$lib/auth.svelte';
	import { ApiError, updateProfile } from '$lib/api';

	let name = $state('');
	let slug = $state('');
	let bio = $state('');
	let website = $state('');
	let location = $state('');
	let password = $state('');
	let loaded = $state(false);

	let saving = $state(false);
	let saveError = $state('');
	let saveFieldErrors = $state<Record<string, string>>({});
	let saveOk = $state(false);

	$effect(() => {
		const user = auth.user;
		if (!user || loaded) return;
		name = user.name;
		slug = user.slug ?? '';
		bio = user.bio ?? '';
		website = user.website ?? '';
		location = user.location ?? '';
		loaded = true;
	});

	async function handleSave(event: SubmitEvent) {
		event.preventDefault();
		saveError = '';
		saveFieldErrors = {};
		saveOk = false;
		saving = true;

		try {
			const payload: {
				name: string;
				slug: string;
				bio: string;
				website: string;
				location: string;
				password?: string;
			} = {
				name,
				slug,
				bio,
				website,
				location
			};
			if (password.trim() !== '') {
				payload.password = password;
			}
			const { user } = await updateProfile(payload);
			auth.user = user;
			name = user.name;
			slug = user.slug;
			bio = user.bio;
			website = user.website;
			location = user.location;
			password = '';
			saveOk = true;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				saveFieldErrors = err.details ?? {};
				saveError = err.message;
			} else if (err instanceof ApiError) {
				saveError = err.message;
			} else {
				saveError = 'Unable to save profile.';
			}
		} finally {
			saving = false;
		}
	}
</script>

<header class="mb-8 pt-2">
	<h1 class="text-[32px] font-medium leading-9 text-stone-900">Profile</h1>
	<p class="mt-2 text-[13px] text-stone-500">
		Your public author profile — name, slug, bio, and links shown on the content API.
	</p>
</header>

<Card.Root class="mt-8 max-w-xl">
	<Card.Header>
		<Card.Title class="text-sm font-medium text-stone-700">Author profile</Card.Title>
	</Card.Header>
	<Card.Content>
		<form class="space-y-4" onsubmit={handleSave} novalidate>
			<div class="space-y-1.5">
				<Label for="profile-name">Name</Label>
				<Input id="profile-name" bind:value={name} aria-invalid={!!saveFieldErrors.name} />
				{#if saveFieldErrors.name}
					<p class="text-xs text-destructive">{saveFieldErrors.name}</p>
				{/if}
			</div>

			<div class="space-y-1.5">
				<Label for="profile-slug">Author slug</Label>
				<Input id="profile-slug" bind:value={slug} aria-invalid={!!saveFieldErrors.slug} />
				<p class="text-xs text-stone-500">Used in <code class="text-[11px]">/api/v1/authors/{'{slug}'}</code>.</p>
				{#if saveFieldErrors.slug}
					<p class="text-xs text-destructive">{saveFieldErrors.slug}</p>
				{/if}
			</div>

			<div class="space-y-1.5">
				<Label for="profile-bio">Bio</Label>
				<textarea
					id="profile-bio"
					class="border-input focus-visible:ring-ring flex min-h-[96px] w-full rounded-md border bg-transparent px-3 py-2 text-sm shadow-xs outline-none focus-visible:ring-1"
					bind:value={bio}
					maxlength="500"
					aria-invalid={!!saveFieldErrors.bio}
				></textarea>
				{#if saveFieldErrors.bio}
					<p class="text-xs text-destructive">{saveFieldErrors.bio}</p>
				{/if}
			</div>

			<div class="space-y-1.5">
				<Label for="profile-website">Website</Label>
				<Input
					id="profile-website"
					type="url"
					placeholder="https://"
					bind:value={website}
					aria-invalid={!!saveFieldErrors.website}
				/>
				{#if saveFieldErrors.website}
					<p class="text-xs text-destructive">{saveFieldErrors.website}</p>
				{/if}
			</div>

			<div class="space-y-1.5">
				<Label for="profile-location">Location</Label>
				<Input
					id="profile-location"
					bind:value={location}
					aria-invalid={!!saveFieldErrors.location}
				/>
				{#if saveFieldErrors.location}
					<p class="text-xs text-destructive">{saveFieldErrors.location}</p>
				{/if}
			</div>

			<div class="space-y-1.5">
				<Label for="profile-password">New password</Label>
				<Input
					id="profile-password"
					type="password"
					autocomplete="new-password"
					bind:value={password}
					aria-invalid={!!saveFieldErrors.password}
				/>
				<p class="text-xs text-stone-500">Leave blank to keep your current password.</p>
				{#if saveFieldErrors.password}
					<p class="text-xs text-destructive">{saveFieldErrors.password}</p>
				{/if}
			</div>

			{#if saveError}
				<p class="text-sm text-destructive" role="alert">{saveError}</p>
			{/if}
			{#if saveOk}
				<p class="text-sm text-stone-600" role="status">Profile saved.</p>
			{/if}

			<Button type="submit" disabled={saving}>
				{saving ? 'Saving…' : 'Save profile'}
			</Button>
		</form>
	</Card.Content>
</Card.Root>

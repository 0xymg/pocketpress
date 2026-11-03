<script lang="ts">
	/**
	 * Media library (plan section 9 routes: /media; section 13 Phase 4):
	 * the shared MediaGrid (search, pagination, upload) plus an inline detail
	 * panel for whichever item is selected — full preview, alt text edit,
	 * copy-URL and delete with confirmation.
	 */
	import * as Card from '$lib/components/ui/card/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import MediaGrid from '$lib/components/media/MediaGrid.svelte';
	import { ApiError, deleteMedia, updateMediaAlt, type Media } from '$lib/api';
	import { formatBytes } from '$lib/utils.js';

	let selected = $state<Media | null>(null);
	let altDraft = $state('');
	let altSaving = $state(false);
	let altError = $state('');

	let deleting = $state(false);
	let confirmingDelete = $state(false);
	let deleteError = $state('');

	let copyFeedback = $state('');

	// Bumped after an out-of-band change (alt save, delete) so MediaGrid
	// re-fetches its current page/search — it owns the list itself.
	let gridRefreshToken = $state(0);

	function describeError(err: unknown): string {
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Something went wrong.';
	}

	function handleSelect(media: Media) {
		selected = media;
		altDraft = media.alt;
		altError = '';
		deleteError = '';
		confirmingDelete = false;
		copyFeedback = '';
	}

	function closeDetail() {
		selected = null;
	}

	async function saveAlt() {
		if (!selected) return;
		altError = '';
		altSaving = true;
		try {
			const { media } = await updateMediaAlt(selected.id, altDraft);
			selected = media;
			altDraft = media.alt;
			gridRefreshToken += 1;
		} catch (err) {
			altError = describeError(err);
		} finally {
			altSaving = false;
		}
	}

	async function copyUrl() {
		if (!selected) return;
		const absolute = new URL(selected.url, window.location.origin).toString();
		try {
			await navigator.clipboard.writeText(absolute);
			copyFeedback = 'Copied!';
		} catch {
			copyFeedback = 'Unable to copy — copy it manually.';
		}
	}

	async function handleDelete() {
		if (!selected) return;
		deleteError = '';
		deleting = true;
		try {
			await deleteMedia(selected.id);
			selected = null;
			confirmingDelete = false;
			gridRefreshToken += 1;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'media_in_use') {
				deleteError = 'Used as a cover/OG image on a post.';
			} else {
				deleteError = describeError(err);
			}
			confirmingDelete = false;
		} finally {
			deleting = false;
		}
	}
</script>

<header class="mb-8 pt-2">
	<h1 class="text-[32px] font-medium leading-9 text-stone-900">Media</h1>
	<p class="mt-2 text-[13px] text-stone-500">Images available to use as post content or artwork.</p>
</header>

<div class="grid gap-6 lg:grid-cols-[1fr_320px]">
			<div>
				<MediaGrid
					onSelect={handleSelect}
					selectedId={selected?.id ?? null}
					refreshToken={gridRefreshToken}
				/>
			</div>

			{#if selected}
				<aside>
					<Card.Root>
						<Card.Content class="space-y-4 pt-6">
							<img
								src={selected.url}
								alt={selected.alt}
								class="w-full rounded-md border border-stone-200 object-cover"
							/>
							<div>
								<p class="truncate font-medium text-stone-900">{selected.filename}</p>
								<p class="text-xs text-stone-500">
									{selected.mime} &middot; {selected.width}&times;{selected.height} &middot; {formatBytes(
										selected.size
									)}
								</p>
							</div>

							<div class="space-y-1.5">
								<Label for="media-alt">Alt text</Label>
								<Input
									id="media-alt"
									bind:value={altDraft}
									onblur={saveAlt}
									aria-invalid={!!altError}
								/>
								{#if altSaving}
									<p class="text-xs text-stone-400">Saving…</p>
								{/if}
								{#if altError}
									<p class="text-xs text-destructive">{altError}</p>
								{/if}
								<Button type="button" variant="outline" size="sm" onclick={saveAlt}>
									Save alt text
								</Button>
							</div>

							<div class="flex items-center gap-2">
								<Button type="button" variant="outline" size="sm" onclick={copyUrl}>
									Copy URL
								</Button>
								{#if copyFeedback}
									<span class="text-xs text-stone-500">{copyFeedback}</span>
								{/if}
							</div>

							<div>
								{#if confirmingDelete}
									<div class="flex gap-2">
										<Button
											variant="destructive"
											size="sm"
											disabled={deleting}
											onclick={handleDelete}
										>
											{deleting ? 'Deleting…' : 'Confirm delete'}
										</Button>
										<Button
											variant="outline"
											size="sm"
											disabled={deleting}
											onclick={() => (confirmingDelete = false)}
										>
											Cancel
										</Button>
									</div>
								{:else}
									<Button
										variant="outline"
										size="sm"
										onclick={() => (confirmingDelete = true)}
									>
										Delete
									</Button>
								{/if}
								{#if deleteError}
									<p class="mt-1 text-xs text-destructive">{deleteError}</p>
								{/if}
							</div>

							<Button type="button" variant="ghost" size="sm" onclick={closeDetail}>
								Close
							</Button>
						</Card.Content>
					</Card.Root>
				</aside>
		{/if}
	</div>

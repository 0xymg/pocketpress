<script lang="ts" module>
	import type { Media } from '$lib/api';

	export interface MediaGridProps {
		/** Called when a thumbnail is clicked/activated — selection (picker) or "open detail" (media page). */
		onSelect?: (media: Media) => void;
		/** Media id to visually highlight as currently selected/active. */
		selectedId?: string | null;
		/** Focus the search input as soon as this grid mounts (used by MediaPicker). */
		autofocusSearch?: boolean;
		/** Page size — defaults to the contract's 40. */
		perPage?: number;
		/** Bump this (e.g. += 1) to force a re-fetch of the current page/search from outside — e.g. after an external alt-text edit or delete. */
		refreshToken?: number;
	}
</script>

<script lang="ts">
	/**
	 * Shared media grid: responsive thumbnail grid + search + pagination +
	 * upload (button and drag-drop), used by both /media (src/routes/media)
	 * and MediaPicker.svelte (plan section 9/13 Phase 4). This component owns
	 * its own data fetching; callers only get an onSelect callback.
	 */
	import { onMount } from 'svelte';
	import { ApiError, listMedia, uploadMedia, type MediaListMeta } from '$lib/api';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { cn, formatBytes } from '$lib/utils.js';

	const SEARCH_DEBOUNCE_MS = 300;
	const ACCEPTED_TYPES = 'image/jpeg,image/png,image/webp,image/gif';

	let {
		onSelect,
		selectedId = null,
		autofocusSearch = false,
		perPage = 40,
		refreshToken = 0
	}: MediaGridProps = $props();

	type LoadState = 'pending' | 'success' | 'error';

	let media = $state<Media[]>([]);
	// Placeholder until the first load() resolves — not read reactively, so a
	// prop literal here (rather than `perPage`) avoids capturing a stale value.
	let meta = $state<MediaListMeta>({ page: 1, per_page: 40, total_items: 0, total_pages: 1 });
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	let searchInput = $state('');
	let debouncedSearch = $state('');
	let page = $state(1);
	let searchTimeout: ReturnType<typeof setTimeout> | undefined;

	let uploading = $state(false);
	let uploadError = $state('');
	let dragDepth = $state(0);
	const isDragging = $derived(dragDepth > 0);

	let fileInputEl: HTMLInputElement | undefined = $state();
	// Input.svelte's bindable `ref` is typed HTMLElement (its WithElementRef
	// default), not HTMLInputElement — match that here rather than casting.
	let searchInputEl: HTMLElement | null = $state(null);

	function handleSearchInput(value: string) {
		searchInput = value;
		clearTimeout(searchTimeout);
		searchTimeout = setTimeout(() => {
			debouncedSearch = value;
			page = 1;
		}, SEARCH_DEBOUNCE_MS);
	}

	async function load() {
		loadState = 'pending';
		try {
			const result = await listMedia({
				page,
				per_page: perPage,
				search: debouncedSearch || undefined
			});
			media = result.media;
			meta = result.meta;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load media.';
			loadState = 'error';
		}
	}

	$effect(() => {
		void debouncedSearch;
		void page;
		void refreshToken;
		load();
	});

	onMount(() => {
		if (autofocusSearch) searchInputEl?.focus();
	});

	function describeUploadError(err: unknown): string {
		if (err instanceof ApiError) {
			return err.details?.file ?? err.message;
		}
		return err instanceof Error ? err.message : 'Upload failed.';
	}

	async function uploadFiles(files: FileList | File[]) {
		const list = Array.from(files);
		if (list.length === 0) return;
		uploadError = '';
		uploading = true;
		try {
			// Sequential on purpose — keeps upload order predictable and avoids
			// hammering the server with parallel multipart requests.
			for (const file of list) {
				await uploadMedia(file);
			}
			await load();
		} catch (err) {
			uploadError = describeUploadError(err);
		} finally {
			uploading = false;
		}
	}

	function handleFileInputChange(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		if (input.files && input.files.length > 0) uploadFiles(input.files);
		input.value = '';
	}

	function handleDragEnter(event: DragEvent) {
		event.preventDefault();
		dragDepth += 1;
	}

	function handleDragOver(event: DragEvent) {
		event.preventDefault();
	}

	function handleDragLeave(event: DragEvent) {
		event.preventDefault();
		dragDepth = Math.max(0, dragDepth - 1);
	}

	function handleDrop(event: DragEvent) {
		event.preventDefault();
		dragDepth = 0;
		const files = event.dataTransfer?.files;
		if (files && files.length > 0) uploadFiles(files);
	}
</script>

<div class="space-y-4">
	<div class="flex flex-wrap items-center justify-between gap-3">
		<div class="min-w-[220px] flex-1 space-y-1.5">
			<label class="sr-only" for="media-search">Search</label>
			<Input
				id="media-search"
				bind:ref={searchInputEl}
				placeholder="Search media…"
				value={searchInput}
				oninput={(event) => handleSearchInput(event.currentTarget.value)}
			/>
		</div>
		<div class="flex items-center gap-2">
			<input
				bind:this={fileInputEl}
				type="file"
				accept={ACCEPTED_TYPES}
				multiple
				class="sr-only"
				aria-label="Upload media"
				onchange={handleFileInputChange}
			/>
			<Button
				type="button"
				size="sm"
				disabled={uploading}
				onclick={() => fileInputEl?.click()}
			>
				{uploading ? 'Uploading…' : 'Upload'}
			</Button>
		</div>
	</div>

	{#if uploadError}
		<p class="text-sm text-destructive" role="alert">{uploadError}</p>
	{/if}

	<div
		class={cn(
			'rounded-md border-2 border-dashed p-3 transition-colors',
			isDragging ? 'border-teal-400 bg-teal-50' : 'border-transparent'
		)}
		role="region"
		aria-label="Media dropzone — drag and drop images here to upload"
		ondragenter={handleDragEnter}
		ondragover={handleDragOver}
		ondragleave={handleDragLeave}
		ondrop={handleDrop}
	>
		{#if isDragging}
			<p class="pb-3 text-center text-sm font-medium text-teal-700">Drop images to upload</p>
		{/if}

		{#if loadState === 'pending' && media.length === 0}
			<p class="p-6 text-center text-sm text-stone-400">Loading media…</p>
		{:else if loadState === 'error'}
			<p class="p-6 text-center text-sm text-destructive">{loadError}</p>
		{:else if media.length === 0}
			<p class="p-6 text-center text-sm text-stone-400">
				No media yet — upload your first image.
			</p>
		{:else}
			<div class="grid grid-cols-2 gap-4 sm:grid-cols-3 md:grid-cols-4 lg:grid-cols-5">
				{#each media as item (item.id)}
					<button
						type="button"
						class={cn(
							'overflow-hidden rounded-md border bg-white text-left',
							item.id === selectedId
								? 'border-teal-500 ring-2 ring-teal-200'
								: 'border-stone-200 hover:border-stone-300'
						)}
						onclick={() => onSelect?.(item)}
					>
						<img
							src={item.url}
							alt={item.alt}
							loading="lazy"
							class="aspect-square w-full bg-stone-100 object-cover"
						/>
						<div class="space-y-0.5 p-2">
							<p class="truncate text-xs font-medium text-stone-700">{item.filename}</p>
							<p class="text-[11px] text-stone-400">
								{item.width}&times;{item.height} &middot; {formatBytes(item.size)}
							</p>
						</div>
					</button>
				{/each}
			</div>
		{/if}
	</div>

	{#if loadState === 'success' && meta.total_pages > 1}
		<div class="flex items-center justify-between">
			<p class="text-sm text-stone-500">
				Page {meta.page} of {meta.total_pages} &middot; {meta.total_items} files
			</p>
			<div class="flex gap-2">
				<Button
					variant="outline"
					size="sm"
					disabled={meta.page <= 1}
					onclick={() => (page = page - 1)}
				>
					Previous
				</Button>
				<Button
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
</div>

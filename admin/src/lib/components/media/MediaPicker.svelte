<script lang="ts">
	/**
	 * Overlay media picker (plan section 13 Phase 4): a fixed, backdropped
	 * panel over the same MediaGrid used by /media, plus an "Upload" affordance
	 * (MediaGrid's own upload button/dropzone — nothing extra needed here).
	 * Escape or a backdrop click cancels. Opening/resolving goes through the
	 * mediaPicker singleton (src/lib/media-picker-state.svelte.ts) rather than
	 * component refs, so PostEditor's cover image, OG image and "insert image"
	 * toolbar action can all share one instance: `await mediaPicker.pick()`.
	 */
	import MediaGrid from './MediaGrid.svelte';
	import { Button } from '$lib/components/ui/button/index.js';
	import { mediaPicker } from '$lib/media-picker-state.svelte';
	import type { Media } from '$lib/api';

	function handleSelect(media: Media) {
		mediaPicker.select(media);
	}

	function handleCancel() {
		mediaPicker.cancel();
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.stopPropagation();
			handleCancel();
		}
	}
</script>

{#if mediaPicker.open}
	<div
		class="fixed inset-0 z-50 flex items-center justify-center p-4"
		role="dialog"
		aria-modal="true"
		aria-label="Select media"
		tabindex="-1"
		onkeydown={handleKeydown}
	>
		<button
			type="button"
			class="fixed inset-0 bg-stone-900/50"
			aria-label="Close media picker"
			onclick={handleCancel}
		></button>

		<div
			class="relative flex max-h-[85vh] w-full max-w-3xl flex-col overflow-hidden rounded-lg bg-white shadow-xl"
		>
			<div class="flex items-center justify-between border-b border-stone-200 px-4 py-3">
				<h2 class="text-sm font-medium text-stone-700">Select media</h2>
				<Button type="button" variant="ghost" size="sm" onclick={handleCancel}>Close</Button>
			</div>
			<div class="overflow-y-auto p-4">
				<MediaGrid onSelect={handleSelect} autofocusSearch />
			</div>
		</div>
	</div>
{/if}

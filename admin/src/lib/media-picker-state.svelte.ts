import type { Media } from './api';

/**
 * Shared singleton state backing MediaPicker.svelte — same rune-based
 * singleton pattern as auth.svelte.ts. A single <MediaPicker /> lives in
 * PostEditor.svelte; any code that wants to prompt the user to choose a
 * media item just awaits mediaPicker.pick(), so the cover image section,
 * the OG image section and the Lexical "insert image" toolbar button can
 * all share one picker instance/promise contract instead of each managing
 * their own dialog.
 */
class MediaPickerStore {
	open = $state(false);
	#resolve: ((value: Media | null) => void) | null = null;

	/** Opens the picker and resolves with the chosen media, or null if cancelled. */
	pick(): Promise<Media | null> {
		this.open = true;
		return new Promise((resolve) => {
			this.#resolve = resolve;
		});
	}

	/** Called by MediaPicker.svelte when the user picks an item. */
	select(media: Media): void {
		this.open = false;
		const resolve = this.#resolve;
		this.#resolve = null;
		resolve?.(media);
	}

	/** Called by MediaPicker.svelte on Escape / backdrop-click / Cancel. */
	cancel(): void {
		this.open = false;
		const resolve = this.#resolve;
		this.#resolve = null;
		resolve?.(null);
	}
}

export const mediaPicker = new MediaPickerStore();

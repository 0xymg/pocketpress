<script lang="ts" module>
	export const popupItemClass =
		'flex w-full items-center gap-2 rounded-[3px] px-2.5 py-[5px] text-left text-[13px] leading-5 text-stone-800 hover:bg-stone-100 disabled:cursor-not-allowed disabled:text-stone-300 disabled:hover:bg-transparent';
</script>

<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		align = 'right',
		trigger,
		children
	}: {
		/** Which edge of the trigger the popup + caret align to. */
		align?: 'left' | 'right';
		/** Trigger button content; receives (toggle, open). */
		trigger: Snippet<[() => void, boolean]>;
		/** Popup content; receives (close) so items can dismiss it. */
		children: Snippet<[() => void]>;
	} = $props();

	let open = $state(false);
	let root: HTMLElement;

	function toggle() {
		open = !open;
	}
	function close() {
		open = false;
	}
	function onWindowPointer(e: MouseEvent) {
		if (open && root && !root.contains(e.target as Node)) close();
	}
	function onKeydown(e: KeyboardEvent) {
		if (e.key === 'Escape') close();
	}

	$effect(() => {
		if (!open) return;
		window.addEventListener('mousedown', onWindowPointer, true);
		window.addEventListener('keydown', onKeydown);
		return () => {
			window.removeEventListener('mousedown', onWindowPointer, true);
			window.removeEventListener('keydown', onKeydown);
		};
	});
</script>

<div bind:this={root} class="relative inline-flex">
	{@render trigger(toggle, open)}
	{#if open}
		<div
			class={'pp-popup absolute top-[calc(100%+10px)] z-40 min-w-[200px] rounded-[4px] border border-stone-100 bg-white p-2.5 ' +
				(align === 'right' ? 'right-0' : 'left-0')}
			role="menu"
			tabindex="-1"
		>
			<span class={'pp-popup-caret ' + (align === 'right' ? 'right-3' : 'left-3')} aria-hidden="true"></span>
			{@render children(close)}
		</div>
	{/if}
</div>

<style>
	.pp-popup {
		box-shadow: 0 -2px 16px -2px rgba(0, 0, 0, 0.2);
	}
	.pp-popup-caret {
		position: absolute;
		top: -16px;
		width: 0;
		height: 0;
		border: 8px solid transparent;
		border-bottom-color: #fff;
	}
</style>

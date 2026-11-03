<script lang="ts">
	import { cn, type WithElementRef } from '$lib/utils.js';
	import type { HTMLTextareaAttributes } from 'svelte/elements';

	let {
		ref = $bindable(null),
		value = $bindable(),
		class: className,
		...restProps
	}: WithElementRef<HTMLTextareaAttributes> = $props();
</script>

<textarea
	bind:this={ref}
	data-slot="textarea"
	class={cn(
		'flex min-h-16 w-full rounded-[3px] border border-stone-200 bg-[var(--theme-input-bg)] px-[15px] py-2 text-[13px] text-stone-900 outline-none transition-colors [box-shadow:0_2px_2px_-1px_rgba(0,0,0,0.1)] placeholder:text-stone-400 hover:border-stone-300 disabled:pointer-events-none disabled:cursor-not-allowed disabled:opacity-50 disabled:[box-shadow:none]',
		'focus-visible:border-stone-400',
		'aria-invalid:border-[var(--theme-error-500)] aria-invalid:bg-[var(--theme-error-50)]',
		className
	)}
	bind:value
	{...restProps}
></textarea>

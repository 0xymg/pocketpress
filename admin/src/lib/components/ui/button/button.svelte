<script lang="ts" module>
	import type { WithElementRef } from '$lib/utils.js';
	import type { HTMLAnchorAttributes, HTMLButtonAttributes } from 'svelte/elements';
	import { tv, type VariantProps } from 'tailwind-variants';

	// Payload-style buttons: near-square (3px radius), no drop shadows, 13px
	// normal-weight text. In-app actions (Create/Save) are light-grey "pill"
	// fills (elevation-150) — that's the `default`. The dark elevation-800 fill
	// is reserved for the big auth CTAs (`primary`). `outline` = Payload's
	// bordered secondary.
	export const buttonVariants = tv({
		base: "inline-flex items-center justify-center gap-2 whitespace-nowrap rounded-[3px] text-[13px] font-normal transition-colors disabled:pointer-events-none disabled:opacity-50 [&_svg]:pointer-events-none [&_svg]:size-4 [&_svg]:shrink-0 outline-none focus-visible:ring-2 focus-visible:ring-stone-900 focus-visible:ring-offset-2 focus-visible:ring-offset-[var(--theme-bg)]",
		variants: {
			variant: {
				default: 'bg-stone-200 text-stone-900 hover:bg-stone-100',
				primary: 'bg-stone-900 text-[var(--theme-elevation-0)] hover:bg-stone-700',
				destructive: 'bg-[var(--theme-error-500)] text-white hover:bg-[var(--theme-error-600)]',
				outline:
					'border border-stone-900 bg-transparent text-stone-900 hover:border-stone-400 hover:text-stone-600',
				secondary:
					'border border-stone-200 bg-stone-100 text-stone-900 hover:bg-stone-200',
				ghost: 'text-stone-900 hover:bg-stone-100',
				link: 'text-stone-900 underline-offset-4 hover:underline'
			},
			size: {
				default: 'h-8 px-3 py-1',
				sm: 'h-[26px] rounded-[3px] px-2',
				lg: 'h-10 rounded-[3px] px-6',
				icon: 'h-8 w-8'
			}
		},
		defaultVariants: {
			variant: 'default',
			size: 'default'
		}
	});

	export type ButtonVariant = VariantProps<typeof buttonVariants>['variant'];
	export type ButtonSize = VariantProps<typeof buttonVariants>['size'];

	export type ButtonProps = WithElementRef<HTMLButtonAttributes> &
		WithElementRef<HTMLAnchorAttributes> & {
			variant?: ButtonVariant;
			size?: ButtonSize;
		};
</script>

<script lang="ts">
	import { cn } from '$lib/utils.js';

	let {
		class: className,
		variant = 'default',
		size = 'default',
		ref = $bindable(null),
		href = undefined,
		type = 'button',
		disabled,
		children,
		...restProps
	}: ButtonProps = $props();
</script>

{#if href}
	<a
		bind:this={ref}
		class={cn(buttonVariants({ variant, size }), className)}
		href={disabled ? undefined : href}
		aria-disabled={disabled}
		role={disabled ? 'link' : undefined}
		tabindex={disabled ? -1 : undefined}
		{...restProps}
	>
		{@render children?.()}
	</a>
{:else}
	<button
		bind:this={ref}
		class={cn(buttonVariants({ variant, size }), className)}
		{type}
		{disabled}
		{...restProps}
	>
		{@render children?.()}
	</button>
{/if}

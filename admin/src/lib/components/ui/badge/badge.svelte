<script lang="ts" module>
	import type { WithElementRef } from '$lib/utils.js';
	import type { HTMLAnchorAttributes, HTMLAttributes } from 'svelte/elements';
	import { tv, type VariantProps } from 'tailwind-variants';

	// Payload-style "pill": small, square-ish, muted fill by default.
	export const badgeVariants = tv({
		base: "inline-flex items-center justify-center rounded-[3px] px-2 py-0.5 text-[12px] font-normal leading-tight w-fit whitespace-nowrap shrink-0 gap-1 [&>svg]:size-3 [&>svg]:pointer-events-none transition-colors overflow-hidden",
		variants: {
			variant: {
				default: 'bg-stone-200 text-stone-900 [a&]:hover:bg-stone-300',
				secondary: 'bg-stone-100 text-stone-900 [a&]:hover:bg-stone-200',
				destructive:
					'bg-[var(--theme-error-150)] text-[var(--theme-error-800)] [a&]:hover:bg-[var(--theme-error-200)]',
				outline:
					'border border-stone-200 bg-[var(--theme-elevation-0)] text-stone-900 [a&]:hover:bg-stone-50'
			}
		},
		defaultVariants: {
			variant: 'default'
		}
	});

	export type BadgeVariant = VariantProps<typeof badgeVariants>['variant'];

	export type BadgeProps = WithElementRef<HTMLAnchorAttributes> &
		WithElementRef<HTMLAttributes<HTMLSpanElement>> & {
			variant?: BadgeVariant;
		};
</script>

<script lang="ts">
	import { cn } from '$lib/utils.js';

	let {
		ref = $bindable(null),
		href,
		class: className,
		variant = 'default',
		children,
		...restProps
	}: BadgeProps = $props();
</script>

<svelte:element
	this={href ? 'a' : 'span'}
	bind:this={ref}
	data-slot="badge"
	{href}
	class={cn(badgeVariants({ variant }), className)}
	{...restProps}
>
	{@render children?.()}
</svelte:element>

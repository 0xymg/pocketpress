<script lang="ts">
	/**
	 * Svelte adaptation of Animate UI's Hugeicons Animate triggers
	 * (https://animate-ui.com/docs/icons/hugeicons + get-started).
	 *
	 * Animate UI ships React + Motion icon files via the shadcn CLI. PocketPress
	 * admin is SvelteKit, so we keep the Hugeicons free stroke-rounded set and
	 * mirror animateOnHover / animateOnTap with the Web Animations API. When a
	 * parent carries data-animate-icon-root (e.g. a nav link), that parent's
	 * pointer events drive the animation — same idea as AnimateIcon asChild.
	 */
	import { onMount } from 'svelte';
	import { HugeiconsIcon, type IconSvgElement } from '@hugeicons/svelte';

	let {
		icon,
		size = 16,
		strokeWidth = 2,
		color = 'currentColor',
		animateOnHover = true,
		animateOnTap = true,
		class: className = ''
	}: {
		icon: IconSvgElement;
		size?: number;
		strokeWidth?: number;
		color?: string;
		animateOnHover?: boolean;
		animateOnTap?: boolean;
		class?: string;
	} = $props();

	let root: HTMLSpanElement | undefined = $state();
	let prefersReducedMotion = $state(false);

	function targetSvg(): Element | null {
		return root?.querySelector('svg') ?? null;
	}

	function play() {
		if (prefersReducedMotion) return;
		const svg = targetSvg();
		if (!svg || !(svg instanceof Element)) return;
		svg.getAnimations().forEach((a) => a.cancel());
		svg.animate(
			[
				{ transform: 'translateY(0px) rotate(0deg)' },
				{ transform: 'translateY(-1.5px) rotate(-7deg)' },
				{ transform: 'translateY(0px) rotate(5deg)' },
				{ transform: 'translateY(-0.5px) rotate(-2deg)' },
				{ transform: 'translateY(0px) rotate(0deg)' }
			],
			{ duration: 450, easing: 'ease-in-out' }
		);
	}

	onMount(() => {
		const mq =
			typeof window.matchMedia === 'function'
				? window.matchMedia('(prefers-reduced-motion: reduce)')
				: null;
		prefersReducedMotion = mq?.matches ?? false;
		const onChange = () => {
			prefersReducedMotion = mq?.matches ?? false;
		};
		mq?.addEventListener('change', onChange);

		const host =
			(root?.closest('[data-animate-icon-root]') as HTMLElement | null) ?? root ?? null;
		if (!host) {
			return () => mq?.removeEventListener('change', onChange);
		}

		const onEnter = () => {
			if (animateOnHover) play();
		};
		const onDown = () => {
			if (animateOnTap) play();
		};

		if (animateOnHover) {
			host.addEventListener('pointerenter', onEnter);
		}
		if (animateOnTap) {
			host.addEventListener('pointerdown', onDown);
		}

		return () => {
			mq?.removeEventListener('change', onChange);
			host.removeEventListener('pointerenter', onEnter);
			host.removeEventListener('pointerdown', onDown);
		};
	});
</script>

<span
	bind:this={root}
	class={className}
	aria-hidden="true"
	style="display: inline-flex; line-height: 0;"
>
	<HugeiconsIcon {icon} {size} {strokeWidth} {color} />
</span>

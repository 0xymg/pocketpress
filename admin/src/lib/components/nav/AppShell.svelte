<script lang="ts">
	/**
	 * Authenticated app chrome: a persistent Payload-style left sidebar plus the
	 * scrolling content region. Rendered once by the root layout for every
	 * signed-in app route (login/setup/splash bypass it).
	 *
	 * The sidebar is instantiated twice — a sticky desktop rail (`hidden
	 * lg:flex`) and a slide-in mobile drawer (`lg:hidden`) — so that at any given
	 * viewport exactly one copy is in the accessibility tree. That keeps
	 * getByRole('link', … ) unambiguous for the e2e golden path.
	 *
	 * The post editor (/posts/new, /posts/{id}) owns its own full-bleed layout,
	 * so it is rendered without the centered content wrapper the other pages get.
	 */
	import type { Snippet } from 'svelte';
	import Sidebar from './Sidebar.svelte';
	import { breadcrumbFromPath, isEditorPath } from '$lib/nav';
	import { auth } from '$lib/auth.svelte';

	let { pathname, children }: { pathname: string; children: Snippet } = $props();

	let drawerOpen = $state(false);
	const isEditor = $derived(isEditorPath(pathname));
	const crumbs = $derived(breadcrumbFromPath(pathname));
	const initial = $derived((auth.user?.name ?? '?').trim().charAt(0).toUpperCase());

	function openDrawer() {
		drawerOpen = true;
	}
	// Nav links inside the drawer close it via onNavigate; the backdrop closes it
	// too. That covers every way the route changes from the drawer.
	function closeDrawer() {
		drawerOpen = false;
	}
</script>

<div class="flex min-h-screen bg-[var(--theme-bg)] text-[var(--theme-text)]">
	<!-- Desktop rail -->
	<aside
		class="sticky top-0 hidden h-screen w-[var(--nav-width)] shrink-0 border-r border-stone-100 bg-[var(--theme-bg)] lg:flex"
	>
		<Sidebar {pathname} />
	</aside>

	<!-- Content column -->
	<div class="flex min-w-0 flex-1 flex-col">
		<!-- Mobile top bar -->
		<header
			class="sticky top-0 z-30 flex h-14 items-center justify-between border-b border-stone-100 bg-[var(--theme-bg)] px-4 lg:hidden"
		>
			<a href="/" class="flex items-center gap-2 text-stone-900 no-underline">
				<span class="h-3.5 w-3.5 rounded-[3px] bg-stone-900" aria-hidden="true"></span>
				<span class="text-sm font-semibold tracking-tight">PocketPress</span>
			</a>
			<button
				type="button"
				aria-label="Open navigation"
				aria-expanded={drawerOpen}
				class="flex h-9 w-9 items-center justify-center rounded-[3px] text-stone-700 hover:bg-stone-50"
				onclick={openDrawer}
			>
				<svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true">
					<path d="M3 6h18M3 12h18M3 18h18" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
				</svg>
			</button>
		</header>

		<!-- Desktop app header: breadcrumb (step-nav) + account avatar -->
		{#if !isEditor}
			<div class="hidden h-14 shrink-0 items-center justify-between px-6 lg:flex lg:px-[60px]">
				<nav aria-label="Breadcrumb" class="flex items-center gap-2 text-[13px] text-stone-800">
					{#each crumbs as crumb, i (crumb.label)}
						{#if i > 0}<span class="text-stone-300">/</span>{/if}
						{#if crumb.href}
							<a href={crumb.href} class="text-stone-500 no-underline hover:text-stone-900">{crumb.label}</a>
						{:else}
							<span>{crumb.label}</span>
						{/if}
					{/each}
				</nav>
				<span
					class="flex h-7 w-7 items-center justify-center rounded-full bg-stone-100 text-[12px] font-medium text-stone-600"
					aria-hidden="true"
				>
					{initial}
				</span>
			</div>
		{/if}

		<main class="flex-1">
			{#if isEditor}
				{@render children()}
			{:else}
				<div class="px-6 pb-16 lg:px-[60px]">
					{@render children()}
				</div>
			{/if}
		</main>
	</div>

	<!-- Mobile drawer + backdrop -->
	<div class="lg:hidden">
		{#if drawerOpen}
			<button
				type="button"
				aria-label="Close navigation"
				class="fixed inset-0 z-40 bg-[var(--theme-overlay)]"
				onclick={closeDrawer}
			></button>
		{/if}
		<aside
			class={'fixed inset-y-0 left-0 z-50 flex w-[var(--nav-width)] max-w-[85vw] flex-col border-r border-stone-100 bg-[var(--theme-bg)] transition-transform duration-150 ' +
				(drawerOpen ? 'translate-x-0' : '-translate-x-full')}
			aria-hidden={!drawerOpen}
		>
			<Sidebar {pathname} onNavigate={closeDrawer} />
		</aside>
	</div>
</div>

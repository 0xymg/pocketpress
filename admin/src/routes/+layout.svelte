<script lang="ts">
	import '../app.css';
	import favicon from '$lib/assets/favicon.svg';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { auth } from '$lib/auth.svelte';
	import { decideRoute } from '$lib/route-guard';
	import AppShell from '$lib/components/nav/AppShell.svelte';

	let { children } = $props();

	// Bootstrap auth state once on mount. auth.init() reads no reactive
	// state itself, so this effect runs exactly once.
	$effect(() => {
		auth.init();
	});

	// Guard flow (plan section 9 — first-run setup): while auth state is
	// loading, decideRoute is a no-op and the splash below stays up.
	// Otherwise it redirects between /setup, /login and the app based on
	// initialized/user, mirroring what the server also enforces.
	$effect(() => {
		const target = decideRoute({
			loading: auth.loading,
			initialized: auth.initialized,
			user: auth.user,
			pathname: page.url.pathname
		});
		if (target && target !== page.url.pathname) {
			goto(target, { replaceState: true });
		}
	});

	const pathname = $derived(page.url.pathname);
	// /login, /setup and the loading splash render full-screen without the
	// sidebar chrome; every other (authenticated) route gets the AppShell.
	const isChromeless = $derived(pathname === '/login' || pathname === '/setup');
</script>

<svelte:head><link rel="icon" href={favicon} /></svelte:head>

{#if auth.loading}
	<div class="flex min-h-screen items-center justify-center bg-[var(--theme-bg)]">
		<p class="text-sm text-stone-400">Loading…</p>
	</div>
{:else if isChromeless}
	{@render children()}
{:else if auth.user}
	<AppShell {pathname}>
		{@render children()}
	</AppShell>
{:else}
	<!-- Transient: signed out on an app route while the guard redirects to /login. -->
	<div class="flex min-h-screen items-center justify-center bg-[var(--theme-bg)]">
		<p class="text-sm text-stone-400">Loading…</p>
	</div>
{/if}

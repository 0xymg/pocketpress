<script lang="ts">
	/**
	 * Persistent left sidebar for the signed-in admin (plan section 9 routes),
	 * styled after Payload's panel nav: a wordmark at the top, muted group
	 * labels, flat text links with a 2px active indicator bar, and a pinned
	 * account block (name, role pill, Logout) at the bottom.
	 *
	 * Rendered once per viewport by AppShell (desktop rail + mobile drawer).
	 * The current pathname is passed in as a prop rather than read from
	 * $app/state here, so this component stays trivially unit-testable and the
	 * active-route logic lives in the pure `activeKeyFromPath` helper.
	 */
	import { goto } from '$app/navigation';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { cn } from '$lib/utils.js';
	import { auth } from '$lib/auth.svelte';
	import { activeKeyFromPath, NAV_GROUPS } from '$lib/nav';

	let { pathname, onNavigate }: { pathname: string; onNavigate?: () => void } = $props();

	const activeKey = $derived(activeKeyFromPath(pathname));

	const visibleGroups = $derived(
		NAV_GROUPS.filter((group) => !group.ownerOnly || auth.user?.role === 'owner')
	);

	let loggingOut = $state(false);

	async function handleLogout() {
		loggingOut = true;
		try {
			await auth.logout();
		} finally {
			loggingOut = false;
			goto('/login');
		}
	}
</script>

<div class="flex h-full flex-col overflow-y-auto px-5 pb-10 pt-5">
	<a
		href="/"
		onclick={onNavigate}
		class="mb-9 flex items-center gap-2.5 text-stone-900 no-underline"
	>
		<span class="h-[18px] w-[18px] shrink-0 rounded-[3px] bg-stone-900" aria-hidden="true"></span>
		<span class="text-[15px] font-semibold tracking-tight">PocketPress</span>
	</a>

	<nav class="flex flex-1 flex-col gap-6" aria-label="Primary">
		{#each visibleGroups as group (group.label)}
			<div class="w-full">
				<p class="mb-1 text-[13px] font-normal text-stone-400">{group.label}</p>
				<ul>
					{#each group.items as item (item.key)}
						<li>
							<a
								href={item.href}
								onclick={onNavigate}
								aria-current={activeKey === item.key ? 'page' : undefined}
								class={cn(
									'relative flex items-center py-[3px] pr-[30px] text-[13px] leading-5 no-underline transition-opacity hover:opacity-70',
									activeKey === item.key ? 'text-stone-900' : 'text-stone-800'
								)}
							>
								{#if activeKey === item.key}
									<span
										class="absolute -left-5 top-1/2 h-4 w-[2px] -translate-y-1/2 rounded-r-[2px] bg-stone-900"
										aria-hidden="true"
									></span>
								{/if}
								{item.label}
							</a>
						</li>
					{/each}
				</ul>
			</div>
		{/each}
	</nav>

	{#if auth.user}
		<div class="mt-6 flex flex-col gap-3">
			<div class="flex items-center justify-between gap-2">
				<div class="min-w-0">
					<p class="truncate text-[13px] font-medium text-stone-900">{auth.user.name}</p>
					<p class="truncate text-[12px] text-stone-400">{auth.user.email}</p>
				</div>
				<Badge variant="secondary" class="shrink-0 capitalize">{auth.user.role}</Badge>
			</div>
			<Button
				variant="ghost"
				size="sm"
				class="w-full justify-start px-0 text-stone-500 hover:bg-transparent hover:text-stone-900"
				disabled={loggingOut}
				onclick={handleLogout}
			>
				{loggingOut ? 'Signing out…' : 'Logout'}
			</Button>
		</div>
	{/if}
</div>

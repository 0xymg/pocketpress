<script lang="ts">
	/**
	 * Dashboard (plan section 9). Kept intentionally small: draft/archived
	 * counts, a short "recently edited" list, and a prominent "New post"
	 * action. Publishing/scheduling counts arrive in Phase 6, so only the
	 * statuses that exist today (draft, archived) are counted. A compact API
	 * status line still proves the SPA can reach the Go backend end to end.
	 */
	import { fetchHealth, listPosts, type HealthStatus, type PostSummary } from '$lib/api';
	import * as Card from '$lib/components/ui/card/index.js';
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';

	type Status = 'pending' | 'success' | 'error';

	let health = $state<HealthStatus | null>(null);
	let healthStatus = $state<Status>('pending');
	let healthError = $state('');

	let draftCount = $state<number | null>(null);
	let scheduledCount = $state<number | null>(null);
	let publishedCount = $state<number | null>(null);
	let archivedCount = $state<number | null>(null);
	let recent = $state<PostSummary[]>([]);
	let postsStatus = $state<Status>('pending');

	$effect(() => {
		let cancelled = false;

		fetchHealth()
			.then((result) => {
				if (cancelled) return;
				health = result;
				healthStatus = 'success';
			})
			.catch((err: unknown) => {
				if (cancelled) return;
				healthError = err instanceof Error ? err.message : '';
				healthStatus = 'error';
			});

		Promise.all([
			listPosts({ status: 'draft', per_page: 1 }),
			listPosts({ status: 'scheduled', per_page: 1 }),
			listPosts({ status: 'published', per_page: 1 }),
			listPosts({ status: 'archived', per_page: 1 }),
			listPosts({ per_page: 5 })
		])
			.then(([drafts, scheduled, published, archived, recentResult]) => {
				if (cancelled) return;
				draftCount = drafts.meta.total_items;
				scheduledCount = scheduled.meta.total_items;
				publishedCount = published.meta.total_items;
				archivedCount = archived.meta.total_items;
				recent = recentResult.posts;
				postsStatus = 'success';
			})
			.catch(() => {
				if (cancelled) return;
				postsStatus = 'error';
			});

		return () => {
			cancelled = true;
		};
	});

	function formatDate(iso: string): string {
		const date = new Date(iso);
		if (Number.isNaN(date.getTime())) return '';
		return date.toLocaleDateString(undefined, { month: 'short', day: 'numeric', year: 'numeric' });
	}
</script>

<header class="mb-8 pt-2">
	<div class="flex flex-wrap items-center gap-3">
		<h1 class="text-[32px] font-medium leading-9 text-stone-900">Dashboard</h1>
		<Button href="/posts/new" size="sm">New post</Button>
	</div>
	<p class="mt-2 text-[13px] text-stone-500">A calm place to write and publish.</p>
</header>

<div class="grid gap-4 grid-cols-2 lg:grid-cols-4">
			<Card.Root>
				<Card.Content class="pt-6">
					<p class="text-sm text-stone-500">Drafts</p>
					<p class="mt-1 text-3xl font-semibold text-stone-900">
						{#if postsStatus === 'success' && draftCount !== null}
							{draftCount}
						{:else if postsStatus === 'error'}
							<span class="text-base text-stone-400">—</span>
						{:else}
							<span class="text-base text-stone-400">…</span>
						{/if}
					</p>
				</Card.Content>
			</Card.Root>
			<a href="/calendar" class="block no-underline">
				<Card.Root class="transition-colors hover:border-stone-300">
					<Card.Content class="pt-6">
						<p class="text-sm text-stone-500">Scheduled</p>
						<p class="mt-1 text-3xl font-semibold text-stone-900">
							{#if postsStatus === 'success' && scheduledCount !== null}
								{scheduledCount}
							{:else if postsStatus === 'error'}
								<span class="text-base text-stone-400">—</span>
							{:else}
								<span class="text-base text-stone-400">…</span>
							{/if}
						</p>
					</Card.Content>
				</Card.Root>
			</a>
			<Card.Root>
				<Card.Content class="pt-6">
					<p class="text-sm text-stone-500">Published</p>
					<p class="mt-1 text-3xl font-semibold text-stone-900">
						{#if postsStatus === 'success' && publishedCount !== null}
							{publishedCount}
						{:else if postsStatus === 'error'}
							<span class="text-base text-stone-400">—</span>
						{:else}
							<span class="text-base text-stone-400">…</span>
						{/if}
					</p>
				</Card.Content>
			</Card.Root>
			<Card.Root>
				<Card.Content class="pt-6">
					<p class="text-sm text-stone-500">Archived</p>
					<p class="mt-1 text-3xl font-semibold text-stone-900">
						{#if postsStatus === 'success' && archivedCount !== null}
							{archivedCount}
						{:else if postsStatus === 'error'}
							<span class="text-base text-stone-400">—</span>
						{:else}
							<span class="text-base text-stone-400">…</span>
						{/if}
					</p>
				</Card.Content>
			</Card.Root>
		</div>

		<Card.Root class="mt-4">
			<Card.Header>
				<Card.Title class="text-sm font-medium text-stone-700">Recently edited</Card.Title>
			</Card.Header>
			<Card.Content>
				{#if postsStatus === 'pending'}
					<p class="text-sm text-stone-400">Loading posts…</p>
				{:else if postsStatus === 'error'}
					<p class="text-sm text-destructive">Unable to load posts.</p>
				{:else if recent.length === 0}
					<p class="text-sm text-stone-400">
						No posts yet. <a href="/posts/new" class="text-primary hover:underline">Write your first.</a>
					</p>
				{:else}
					<ul class="divide-y divide-stone-100">
						{#each recent as post (post.id)}
							<li>
								<a
									href={`/posts/${post.id}`}
									class="flex items-center justify-between gap-3 py-2 hover:text-stone-900"
								>
									<span class="truncate text-sm font-medium text-stone-700">{post.title}</span>
									<span class="flex shrink-0 items-center gap-2">
										<Badge variant="secondary" class="capitalize">{post.status}</Badge>
										<span class="text-xs text-stone-400">{formatDate(post.updated_at)}</span>
									</span>
								</a>
							</li>
						{/each}
					</ul>
				{/if}
			</Card.Content>
		</Card.Root>

		<Card.Root class="mt-4">
			<Card.Content class="flex items-center justify-between gap-3 pt-6">
				<span class="text-sm text-stone-500">API status</span>
				{#if healthStatus === 'pending'}
					<span class="text-sm text-stone-400">Checking API status…</span>
				{:else if healthStatus === 'error'}
					<span class="text-sm text-destructive">
						Unable to reach the API{healthError ? `: ${healthError}` : ''}
					</span>
				{:else if health}
					<span class="flex items-center gap-2">
						<Badge>{health.status}</Badge>
						<span class="text-sm font-medium text-stone-900">{health.version}</span>
					</span>
				{/if}
			</Card.Content>
		</Card.Root>


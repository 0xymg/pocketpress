<script lang="ts">
	/**
	 * Draft review queue (v0.3): lists posts with review_status=pending.
	 * Approve / request-changes / open editor from here.
	 */
	import { Button } from '$lib/components/ui/button/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import {
		ApiError,
		approveReview,
		listPosts,
		requestChanges,
		withdrawReview,
		type PostSummary
	} from '$lib/api';

	type LoadState = 'pending' | 'success' | 'error';

	let posts = $state<PostSummary[]>([]);
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');
	let rowBusy = $state<Record<string, boolean>>({});
	let rowError = $state<Record<string, string>>({});
	let noteOpenId = $state<string | null>(null);
	let noteText = $state('');
	let noteFieldError = $state('');

	async function load() {
		loadState = 'pending';
		loadError = '';
		try {
			const result = await listPosts({
				review_status: 'pending',
				per_page: 50,
				sort: '-updated_at'
			});
			posts = result.posts;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load reviews.';
			loadState = 'error';
		}
	}

	$effect(() => {
		load();
	});

	function setBusy(id: string, busy: boolean) {
		rowBusy = { ...rowBusy, [id]: busy };
	}

	function setError(id: string, message: string) {
		rowError = { ...rowError, [id]: message };
	}

	async function handleApprove(id: string) {
		setBusy(id, true);
		setError(id, '');
		try {
			await approveReview(id);
			posts = posts.filter((p) => p.id !== id);
		} catch (err) {
			setError(id, err instanceof ApiError ? err.message : 'Approve failed.');
		} finally {
			setBusy(id, false);
		}
	}

	function openNote(id: string) {
		noteOpenId = id;
		noteText = '';
		noteFieldError = '';
		setError(id, '');
	}

	async function confirmRequestChanges() {
		if (!noteOpenId) return;
		const id = noteOpenId;
		const note = noteText.trim();
		if (!note) {
			noteFieldError = 'A note is required.';
			return;
		}
		setBusy(id, true);
		noteFieldError = '';
		try {
			await requestChanges(id, { note });
			posts = posts.filter((p) => p.id !== id);
			noteOpenId = null;
		} catch (err) {
			setError(id, err instanceof ApiError ? err.message : 'Request changes failed.');
		} finally {
			setBusy(id, false);
		}
	}

	async function handleWithdraw(id: string) {
		setBusy(id, true);
		setError(id, '');
		try {
			await withdrawReview(id);
			posts = posts.filter((p) => p.id !== id);
		} catch (err) {
			setError(id, err instanceof ApiError ? err.message : 'Withdraw failed.');
		} finally {
			setBusy(id, false);
		}
	}

	function formatWhen(iso: string | null): string {
		if (!iso) return '—';
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return iso;
		return d.toLocaleString();
	}
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight text-stone-900">Reviews</h1>
			<p class="mt-1 text-sm text-stone-500">Posts waiting for editorial approval.</p>
		</div>
		<Button variant="outline" size="sm" onclick={load} disabled={loadState === 'pending'}>
			Refresh
		</Button>
	</div>

	{#if loadState === 'pending'}
		<p class="text-sm text-stone-500">Loading…</p>
	{:else if loadState === 'error'}
		<p class="text-sm text-destructive" role="alert">{loadError}</p>
	{:else if posts.length === 0}
		<p class="text-sm text-stone-500">No posts pending review.</p>
	{:else}
		<ul class="divide-y divide-stone-100 rounded-md border border-stone-200 bg-white">
			{#each posts as post (post.id)}
				<li class="flex flex-col gap-3 px-4 py-4 sm:flex-row sm:items-start sm:justify-between">
					<div class="min-w-0 space-y-1">
						<a
							href={`/posts/${post.id}`}
							class="block truncate text-[15px] font-semibold text-stone-900 hover:underline"
						>
							{post.title || '(untitled)'}
						</a>
						<p class="text-xs text-stone-500">
							{post.author.name} · submitted {formatWhen(post.submitted_for_review_at)}
						</p>
						{#if rowError[post.id]}
							<p class="text-xs text-destructive" role="alert">{rowError[post.id]}</p>
						{/if}
						{#if noteOpenId === post.id}
							<div class="mt-2 space-y-2 rounded-md border border-stone-200 bg-stone-50 p-3">
								<Label for={`note-${post.id}`}>Changes requested note</Label>
								<Textarea id={`note-${post.id}`} class="min-h-20" bind:value={noteText} />
								{#if noteFieldError}
									<p class="text-xs text-destructive">{noteFieldError}</p>
								{/if}
								<div class="flex flex-wrap gap-2">
									<Button size="sm" disabled={rowBusy[post.id]} onclick={confirmRequestChanges}>
										{rowBusy[post.id] ? 'Sending…' : 'Send'}
									</Button>
									<Button
										variant="outline"
										size="sm"
										disabled={rowBusy[post.id]}
										onclick={() => (noteOpenId = null)}
									>
										Cancel
									</Button>
								</div>
							</div>
						{/if}
					</div>
					<div class="flex shrink-0 flex-wrap gap-2">
						<Button
							variant="primary"
							size="sm"
							disabled={rowBusy[post.id]}
							onclick={() => handleApprove(post.id)}
						>
							Approve
						</Button>
						<Button
							variant="outline"
							size="sm"
							disabled={rowBusy[post.id]}
							onclick={() => openNote(post.id)}
						>
							Request changes
						</Button>
						<Button
							variant="outline"
							size="sm"
							disabled={rowBusy[post.id]}
							onclick={() => handleWithdraw(post.id)}
						>
							Withdraw
						</Button>
					</div>
				</li>
			{/each}
		</ul>
	{/if}
</div>

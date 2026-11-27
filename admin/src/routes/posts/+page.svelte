<script lang="ts">
	/**
	 * Post list (plan section 9), Payload-style: a search + Columns/Filters
	 * controls row, a leading selection checkbox column with a "N selected"
	 * bulk-delete toolbar, sortable Title/Updated headers, per-row quick actions
	 * and pagination.
	 */
	import { Button } from '$lib/components/ui/button/index.js';
	import { Popup } from '$lib/components/ui/popup/index.js';
	import {
		ApiError,
		deletePost,
		duplicatePost,
		importMarkdownFiles,
		listCategories,
		listPosts,
		type Category,
		type PostListMeta,
		type PostStatus,
		type PostSummary
	} from '$lib/api';

	const SEARCH_DEBOUNCE_MS = 300;
	const PER_PAGE = 20;

	type LoadState = 'pending' | 'success' | 'error';
	type SortKey = 'updated_at' | 'title';

	const COLUMNS = [
		{ key: 'status', label: 'Status' },
		{ key: 'scheduled', label: 'Scheduled' },
		{ key: 'author', label: 'Author' },
		{ key: 'updated', label: 'Updated' }
	] as const;
	type ColKey = (typeof COLUMNS)[number]['key'];
	const COLS_STORAGE = 'pp:posts:columns';

	let posts = $state<PostSummary[]>([]);
	// Note: loadPosts() reads PER_PAGE (a plain constant), never meta.per_page,
	// for its request params — meta is reassigned wholesale from the response, so
	// reading it back would loop the fetch effect.
	let meta = $state<PostListMeta>({ page: 1, per_page: PER_PAGE, total_items: 0, total_pages: 1 });
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	let categories = $state<Category[]>([]);

	let searchInput = $state('');
	let debouncedSearch = $state('');
	let statusFilter = $state<'all' | PostStatus>('all');
	let categoryFilter = $state('');
	let page = $state(1);
	let sortKey = $state<SortKey>('updated_at');
	let sortDesc = $state(true);

	let rowBusy = $state<Record<string, boolean>>({});
	let rowError = $state<Record<string, string>>({});
	let confirmDeleteId = $state<string | null>(null);

	// --- selection (bulk delete) ---
	let selectedIds = $state<Set<string>>(new Set());
	let confirmBulk = $state(false);
	let bulkDeleting = $state(false);
	let bulkError = $state('');
	let selectAllEl = $state<HTMLInputElement | undefined>();

	let importInputEl = $state<HTMLInputElement | undefined>();
	let importing = $state(false);
	let importMessage = $state('');
	let importError = $state('');

	const allSelected = $derived(posts.length > 0 && posts.every((p) => selectedIds.has(p.id)));
	const someSelected = $derived(posts.some((p) => selectedIds.has(p.id)));

	$effect(() => {
		if (selectAllEl) selectAllEl.indeterminate = someSelected && !allSelected;
	});

	// --- column visibility (persisted) ---
	let visibleColumns = $state<Record<ColKey, boolean>>({
		status: true,
		scheduled: true,
		author: true,
		updated: true
	});

	function loadColumns() {
		if (typeof localStorage === 'undefined') return;
		try {
			const raw = localStorage.getItem(COLS_STORAGE);
			if (raw) visibleColumns = { ...visibleColumns, ...JSON.parse(raw) };
		} catch {
			// Corrupt/absent preference — keep the all-visible default.
		}
	}
	function toggleColumn(key: ColKey) {
		visibleColumns = { ...visibleColumns, [key]: !visibleColumns[key] };
		try {
			localStorage?.setItem(COLS_STORAGE, JSON.stringify(visibleColumns));
		} catch {
			// Persistence is best-effort.
		}
	}
	$effect(() => {
		loadColumns();
	});

	let searchTimeout: ReturnType<typeof setTimeout> | undefined;

	function handleSearchInput(value: string) {
		searchInput = value;
		clearTimeout(searchTimeout);
		searchTimeout = setTimeout(() => {
			debouncedSearch = value;
			page = 1;
		}, SEARCH_DEBOUNCE_MS);
	}

	function handleStatusChange(value: 'all' | PostStatus) {
		statusFilter = value;
		page = 1;
	}

	function handleCategoryChange(value: string) {
		categoryFilter = value;
		page = 1;
	}

	function sortBy(key: SortKey, desc: boolean) {
		sortKey = key;
		sortDesc = desc;
		page = 1;
	}

	function isActiveSort(key: SortKey, desc: boolean): boolean {
		return sortKey === key && sortDesc === desc;
	}

	async function loadPosts() {
		loadState = 'pending';
		selectedIds = new Set();
		confirmBulk = false;
		try {
			const result = await listPosts({
				page,
				per_page: PER_PAGE,
				status: statusFilter === 'all' ? undefined : statusFilter,
				category: categoryFilter || undefined,
				search: debouncedSearch || undefined,
				sort: `${sortDesc ? '-' : ''}${sortKey}`
			});
			posts = result.posts;
			meta = result.meta;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load posts.';
			loadState = 'error';
		}
	}

	async function loadCategories() {
		try {
			const { categories: fetched } = await listCategories();
			categories = fetched;
		} catch {
			// The category filter just renders "All categories" if this fails.
		}
	}

	$effect(() => {
		loadCategories();
	});

	$effect(() => {
		// Re-fetch whenever any filter, the sort or the page changes.
		void debouncedSearch;
		void statusFilter;
		void categoryFilter;
		void sortKey;
		void sortDesc;
		void page;
		loadPosts();
	});

	function describeError(err: unknown): string {
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Something went wrong.';
	}

	function toggleSelect(id: string) {
		const next = new Set(selectedIds);
		if (next.has(id)) next.delete(id);
		else next.add(id);
		selectedIds = next;
	}

	function toggleSelectAll() {
		selectedIds = allSelected ? new Set() : new Set(posts.map((p) => p.id));
	}

	async function bulkDelete() {
		bulkError = '';
		bulkDeleting = true;
		try {
			for (const id of selectedIds) {
				await deletePost(id);
			}
			confirmBulk = false;
			await loadPosts();
		} catch (err) {
			bulkError = describeError(err);
		} finally {
			bulkDeleting = false;
		}
	}

	function openMarkdownImport() {
		importMessage = '';
		importError = '';
		importInputEl?.click();
	}

	async function handleMarkdownImport(event: Event) {
		const input = event.currentTarget as HTMLInputElement;
		const files = input.files ? Array.from(input.files) : [];
		input.value = '';
		if (files.length === 0) return;

		importing = true;
		importMessage = '';
		importError = '';
		try {
			const result = await importMarkdownFiles(files);
			const ok = result.imported?.length ?? 0;
			const fail = result.failed?.length ?? 0;
			if (ok > 0) {
				await loadPosts();
			}
			if (fail === 0) {
				importMessage = ok === 1 ? 'Imported 1 post from Markdown.' : `Imported ${ok} posts from Markdown.`;
			} else if (ok === 0) {
				const first = result.failed[0];
				importError = first?.message
					? `Import failed: ${first.message}`
					: 'Import failed.';
			} else {
				importMessage = `Imported ${ok} post${ok === 1 ? '' : 's'}; ${fail} failed.`;
				const first = result.failed[0];
				if (first?.filename) {
					importError = `${first.filename}: ${first.message}`;
				}
			}
		} catch (err) {
			importError = describeError(err);
		} finally {
			importing = false;
		}
	}

	async function handleDuplicate(post: PostSummary) {
		rowError = { ...rowError, [post.id]: '' };
		rowBusy = { ...rowBusy, [post.id]: true };
		try {
			await duplicatePost(post.id);
			await loadPosts();
		} catch (err) {
			rowError = { ...rowError, [post.id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [post.id]: false };
		}
	}

	async function handleDelete(post: PostSummary) {
		rowError = { ...rowError, [post.id]: '' };
		rowBusy = { ...rowBusy, [post.id]: true };
		try {
			await deletePost(post.id);
			confirmDeleteId = null;
			await loadPosts();
		} catch (err) {
			rowError = { ...rowError, [post.id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [post.id]: false };
		}
	}

	function formatDate(value: string): string {
		return new Date(value).toLocaleDateString(undefined, {
			year: 'numeric',
			month: 'short',
			day: 'numeric'
		});
	}

	function formatDateTime(value: string): string {
		return new Date(value).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
	}
</script>

{#snippet sortHead(label: string, key: SortKey)}
	<div class="flex items-center gap-1.5">
		<span>{label}</span>
		<span class="flex flex-col leading-[0]">
			<button
				type="button"
				aria-label={`Sort by ${label} ascending`}
				class={isActiveSort(key, false) ? 'text-stone-700' : 'text-stone-300 hover:text-stone-500'}
				onclick={() => sortBy(key, false)}
			>
				<svg width="12" height="8" viewBox="0 0 20 12" fill="none" aria-hidden="true"><path d="M14 8 10 4 6 8" stroke="currentColor" stroke-width="2" stroke-linecap="square" /></svg>
			</button>
			<button
				type="button"
				aria-label={`Sort by ${label} descending`}
				class={isActiveSort(key, true) ? 'text-stone-700' : 'text-stone-300 hover:text-stone-500'}
				onclick={() => sortBy(key, true)}
			>
				<svg width="12" height="8" viewBox="0 0 20 12" fill="none" aria-hidden="true"><path d="M6 4l4 4 4-4" stroke="currentColor" stroke-width="2" stroke-linecap="square" /></svg>
			</button>
		</span>
	</div>
{/snippet}

<header class="mb-6 flex flex-wrap items-start justify-between gap-3 pt-2">
	<div>
		<div class="flex flex-wrap items-center gap-3">
			<h1 class="text-[32px] font-medium leading-9 text-stone-900">Posts</h1>
			<Button href="/posts/new" size="sm">New post</Button>
			<Button
				type="button"
				variant="outline"
				size="sm"
				disabled={importing}
				onclick={openMarkdownImport}
			>
				{importing ? 'Importing…' : 'Import Markdown'}
			</Button>
			<input
				bind:this={importInputEl}
				type="file"
				accept=".md,.markdown,text/markdown"
				multiple
				class="hidden"
				aria-hidden="true"
				tabindex="-1"
				onchange={handleMarkdownImport}
			/>
		</div>
		<p class="mt-2 text-[13px] text-stone-500">Everything you've written, drafts included.</p>
		{#if importMessage}
			<p class="mt-2 text-[13px] text-stone-600" role="status">{importMessage}</p>
		{/if}
		{#if importError}
			<p class="mt-2 text-[13px] text-destructive" role="alert">{importError}</p>
		{/if}
	</div>
	{#if selectedIds.size > 0}
		<div class="flex items-center gap-2 text-[13px] text-stone-500">
			<span>{selectedIds.size} selected</span>
			<span class="text-stone-300">—</span>
			{#if confirmBulk}
				<button
					type="button"
					class="text-destructive underline underline-offset-2 disabled:opacity-50"
					disabled={bulkDeleting}
					onclick={bulkDelete}
				>
					{bulkDeleting ? 'Deleting…' : 'Confirm delete'}
				</button>
				<button
					type="button"
					class="text-stone-700 underline underline-offset-2"
					disabled={bulkDeleting}
					onclick={() => (confirmBulk = false)}
				>
					Cancel
				</button>
			{:else}
				<button
					type="button"
					class="text-stone-800 underline underline-offset-2 hover:text-stone-900"
					onclick={() => (confirmBulk = true)}
				>
					Delete
				</button>
			{/if}
		</div>
	{/if}
</header>

{#if bulkError}
	<p class="mb-3 text-[13px] text-destructive" role="alert">{bulkError}</p>
{/if}

<div class="mb-4 flex flex-wrap items-center gap-3">
	<div class="relative min-w-[240px] flex-1">
		<svg
			class="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-stone-400"
			width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"
		>
			<circle cx="11" cy="11" r="7" stroke="currentColor" stroke-width="2" />
			<path d="m20 20-3.5-3.5" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
		</svg>
		<label for="post-search" class="sr-only">Search</label>
		<input
			id="post-search"
			placeholder="Search by Title"
			value={searchInput}
			oninput={(event) => handleSearchInput(event.currentTarget.value)}
			class="h-11 w-full rounded-[3px] bg-stone-50 pl-10 pr-4 text-[13px] text-stone-900 outline-none transition-colors placeholder:text-stone-400 hover:bg-stone-100 focus:bg-stone-100"
		/>
	</div>

	<Popup align="right">
		{#snippet trigger(toggle: () => void, open: boolean)}
			<button
				type="button"
				aria-expanded={open}
				onclick={toggle}
				class="flex h-9 items-center gap-1.5 rounded-[3px] border border-stone-200 bg-white px-3 text-[13px] text-stone-700 outline-none hover:border-stone-300"
			>
				Columns
				<svg width="12" height="12" viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M6 8l4 4 4-4" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
			</button>
		{/snippet}
		{#snippet children(_close: () => void)}
			<p class="mb-2 px-1 text-[10px] text-stone-400">Toggle columns</p>
			<div class="flex max-w-[220px] flex-wrap gap-1.5">
				{#each COLUMNS as col (col.key)}
					<button
						type="button"
						aria-pressed={visibleColumns[col.key]}
						onclick={() => toggleColumn(col.key)}
						class={'inline-flex items-center gap-1 rounded-[3px] px-2 py-1 text-[12px] ' +
							(visibleColumns[col.key]
								? 'bg-stone-100 text-stone-800'
								: 'border border-stone-200 text-stone-400 hover:text-stone-600')}
					>
						{col.label}
						{#if visibleColumns[col.key]}
							<svg width="11" height="11" viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M14 6 6 14M6 6l8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
						{/if}
					</button>
				{/each}
			</div>
		{/snippet}
	</Popup>

	<label for="status-filter" class="sr-only">Status</label>
	<select
		id="status-filter"
		class="h-9 rounded-[3px] border border-stone-200 bg-white px-3 text-[13px] text-stone-700 outline-none hover:border-stone-300"
		value={statusFilter}
		onchange={(event) => handleStatusChange(event.currentTarget.value as 'all' | PostStatus)}
	>
		<option value="all">All statuses</option>
		<option value="draft">Draft</option>
		<option value="scheduled">Scheduled</option>
		<option value="published">Published</option>
		<option value="archived">Archived</option>
	</select>
	<label for="category-filter" class="sr-only">Category</label>
	<select
		id="category-filter"
		class="h-9 rounded-[3px] border border-stone-200 bg-white px-3 text-[13px] text-stone-700 outline-none hover:border-stone-300"
		value={categoryFilter}
		onchange={(event) => handleCategoryChange(event.currentTarget.value)}
	>
		<option value="">All categories</option>
		{#each categories as category (category.id)}
			<option value={category.id}>{category.name}</option>
		{/each}
	</select>
</div>

{#if loadState === 'pending'}
	<p class="py-6 text-[13px] text-stone-400">Loading posts…</p>
{:else if loadState === 'error'}
	<p class="py-6 text-[13px] text-destructive">{loadError}</p>
{:else if posts.length === 0}
	<p class="py-6 text-[13px] text-stone-400">No posts match these filters yet.</p>
{:else}
	<div class="overflow-x-auto">
		<table class="w-full min-w-[820px] border-collapse text-left text-[13px]">
			<thead>
				<tr class="text-stone-400">
					<th class="w-8 px-3 py-3 pl-4">
						<input
							type="checkbox"
							aria-label="Select all posts"
							bind:this={selectAllEl}
							checked={allSelected}
							onchange={toggleSelectAll}
							class="h-4 w-4 accent-stone-700"
						/>
					</th>
					<th class="px-3 py-3 font-normal">{@render sortHead('Title', 'title')}</th>
					{#if visibleColumns.status}<th class="px-3 py-3 font-normal">Status</th>{/if}
					{#if visibleColumns.scheduled}<th class="px-3 py-3 font-normal">Scheduled</th>{/if}
					{#if visibleColumns.author}<th class="px-3 py-3 font-normal">Author</th>{/if}
					{#if visibleColumns.updated}
						<th class="px-3 py-3 font-normal">{@render sortHead('Updated', 'updated_at')}</th>
					{/if}
					<th class="px-3 py-3 pr-4 font-normal">Actions</th>
				</tr>
			</thead>
			<tbody>
				{#each posts as post (post.id)}
					<tr class="group align-middle">
						<td class="rounded-l-[3px] px-3 py-3 pl-4 group-hover:bg-stone-50">
							<input
								type="checkbox"
								aria-label={`Select ${post.title}`}
								checked={selectedIds.has(post.id)}
								onchange={() => toggleSelect(post.id)}
								class="h-4 w-4 accent-stone-700"
							/>
						</td>
						<td class="max-w-sm px-3 py-3 group-hover:bg-stone-50">
							<a href="/posts/{post.id}" class="text-stone-900 underline-offset-2 hover:underline"
								>{post.title}</a
							>
							{#if post.excerpt}
								<p class="mt-0.5 truncate text-[12px] text-stone-400">{post.excerpt}</p>
							{/if}
						</td>
						{#if visibleColumns.status}
							<td class="px-3 py-3 capitalize text-stone-700 group-hover:bg-stone-50">{post.status}</td>
						{/if}
						{#if visibleColumns.scheduled}
							<td class="px-3 py-3 text-stone-500 group-hover:bg-stone-50">
								{#if post.status === 'scheduled' && post.scheduled_for}
									{formatDateTime(post.scheduled_for)}
								{:else}
									&mdash;
								{/if}
							</td>
						{/if}
						{#if visibleColumns.author}
							<td class="px-3 py-3 text-stone-600 group-hover:bg-stone-50">{post.author.name}</td>
						{/if}
						{#if visibleColumns.updated}
							<td class="px-3 py-3 text-stone-500 group-hover:bg-stone-50">{formatDate(post.updated_at)}</td>
						{/if}
						<td class="rounded-r-[3px] px-3 py-3 pr-4 group-hover:bg-stone-50">
							<div class="flex items-center gap-2">
								<Button
									variant="outline"
									size="sm"
									disabled={rowBusy[post.id]}
									onclick={() => handleDuplicate(post)}
								>
									Duplicate
								</Button>
								{#if confirmDeleteId === post.id}
									<Button
										variant="destructive"
										size="sm"
										disabled={rowBusy[post.id]}
										onclick={() => handleDelete(post)}
									>
										Confirm
									</Button>
									<Button
										variant="outline"
										size="sm"
										disabled={rowBusy[post.id]}
										onclick={() => (confirmDeleteId = null)}
									>
										Cancel
									</Button>
								{:else}
									<Button
										variant="outline"
										size="sm"
										disabled={rowBusy[post.id]}
										onclick={() => (confirmDeleteId = post.id)}
									>
										Delete
									</Button>
								{/if}
							</div>
							{#if rowError[post.id]}
								<p class="mt-1 text-[12px] text-destructive">{rowError[post.id]}</p>
							{/if}
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>

	<div class="mt-4 flex items-center justify-end gap-4 text-[13px] text-stone-500">
		{#if meta.total_pages > 1}
			<div class="flex items-center gap-1">
				<button
					type="button"
					class="flex h-[30px] w-[30px] items-center justify-center rounded-[3px] text-stone-700 hover:bg-stone-50 disabled:opacity-40"
					disabled={meta.page <= 1}
					aria-label="Previous page"
					onclick={() => (page = page - 1)}>‹</button
				>
				{#each Array(meta.total_pages) as _, i (i)}
					<button
						type="button"
						aria-current={meta.page === i + 1 ? 'page' : undefined}
						class="flex h-[30px] w-[30px] items-center justify-center rounded-[3px] {meta.page === i + 1
							? 'bg-stone-100 text-stone-400'
							: 'text-stone-700 hover:bg-stone-50'}"
						onclick={() => (page = i + 1)}>{i + 1}</button
					>
				{/each}
				<button
					type="button"
					class="flex h-[30px] w-[30px] items-center justify-center rounded-[3px] text-stone-700 hover:bg-stone-50 disabled:opacity-40"
					disabled={meta.page >= meta.total_pages}
					aria-label="Next page"
					onclick={() => (page = page + 1)}>›</button
				>
			</div>
		{/if}
		<span>
			{(meta.page - 1) * meta.per_page + 1}&ndash;{Math.min(
				meta.page * meta.per_page,
				meta.total_items
			)} of {meta.total_items}
		</span>
		<span class="font-medium text-stone-700">Per Page: {meta.per_page}</span>
	</div>
{/if}

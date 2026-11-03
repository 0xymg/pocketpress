<script lang="ts">
	/**
	 * Tag management (plan section 9 routes): identical shape to /categories,
	 * minus the description field tags don't have.
	 */
	import * as Card from '$lib/components/ui/card/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { ApiError, createTag, deleteTag, listTags, updateTag, type Tag } from '$lib/api';

	type LoadState = 'pending' | 'success' | 'error';

	let tags = $state<Tag[]>([]);
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	let newName = $state('');
	let newSlug = $state('');
	let creating = $state(false);
	let createError = $state('');
	let createFieldErrors = $state<Record<string, string>>({});

	let editingId = $state<string | null>(null);
	let editName = $state('');
	let editSlug = $state('');
	let editFieldErrors = $state<Record<string, string>>({});
	let rowBusy = $state<Record<string, boolean>>({});
	let rowError = $state<Record<string, string>>({});
	let confirmDeleteId = $state<string | null>(null);

	async function loadTags() {
		loadState = 'pending';
		try {
			const { tags: fetched } = await listTags();
			tags = fetched;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load tags.';
			loadState = 'error';
		}
	}

	$effect(() => {
		loadTags();
	});

	function describeError(err: unknown): string {
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Something went wrong.';
	}

	async function handleCreate(event: SubmitEvent) {
		event.preventDefault();
		createError = '';
		createFieldErrors = {};
		creating = true;
		try {
			const { tag } = await createTag({ name: newName, slug: newSlug || undefined });
			tags = [...tags, tag];
			newName = '';
			newSlug = '';
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				createFieldErrors = err.details ?? {};
			}
			createError = describeError(err);
		} finally {
			creating = false;
		}
	}

	function startEdit(tag: Tag) {
		editingId = tag.id;
		editName = tag.name;
		editSlug = tag.slug;
		editFieldErrors = {};
		rowError = { ...rowError, [tag.id]: '' };
	}

	function cancelEdit() {
		editingId = null;
		editFieldErrors = {};
	}

	async function saveEdit(id: string) {
		editFieldErrors = {};
		rowBusy = { ...rowBusy, [id]: true };
		try {
			const { tag } = await updateTag(id, { name: editName, slug: editSlug });
			tags = tags.map((t) => (t.id === id ? tag : t));
			editingId = null;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				editFieldErrors = err.details ?? {};
			}
			rowError = { ...rowError, [id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [id]: false };
		}
	}

	async function handleDelete(id: string) {
		rowBusy = { ...rowBusy, [id]: true };
		rowError = { ...rowError, [id]: '' };
		try {
			await deleteTag(id);
			tags = tags.filter((t) => t.id !== id);
			confirmDeleteId = null;
		} catch (err) {
			rowError = { ...rowError, [id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [id]: false };
		}
	}
</script>

<header class="mb-8 pt-2">
	<h1 class="text-[32px] font-medium leading-9 text-stone-900">Tags</h1>
	<p class="mt-2 text-[13px] text-stone-500">Fine-grained labels for your posts.</p>
</header>

<Card.Root>
			<Card.Header>
				<Card.Title class="text-sm font-medium text-stone-700">All tags</Card.Title>
			</Card.Header>
			<Card.Content>
				{#if loadState === 'pending'}
					<p class="text-sm text-stone-400">Loading tags…</p>
				{:else if loadState === 'error'}
					<p class="text-sm text-destructive">{loadError}</p>
				{:else if tags.length === 0}
					<p class="text-sm text-stone-400">No tags yet — add your first one below.</p>
				{:else}
					<ul class="divide-y divide-stone-100">
						{#each tags as tag (tag.id)}
							<li class="py-3">
								{#if editingId === tag.id}
									<div class="space-y-2">
										<div class="grid gap-2 sm:grid-cols-2">
											<div>
												<Label for="edit-name-{tag.id}">Name</Label>
												<Input
													id="edit-name-{tag.id}"
													bind:value={editName}
													aria-invalid={!!editFieldErrors.name}
												/>
												{#if editFieldErrors.name}
													<p class="text-xs text-destructive">{editFieldErrors.name}</p>
												{/if}
											</div>
											<div>
												<Label for="edit-slug-{tag.id}">Slug</Label>
												<Input
													id="edit-slug-{tag.id}"
													bind:value={editSlug}
													aria-invalid={!!editFieldErrors.slug}
												/>
												{#if editFieldErrors.slug}
													<p class="text-xs text-destructive">{editFieldErrors.slug}</p>
												{/if}
											</div>
										</div>
										<div class="flex gap-2">
											<Button size="sm" disabled={rowBusy[tag.id]} onclick={() => saveEdit(tag.id)}>
												{rowBusy[tag.id] ? 'Saving…' : 'Save'}
											</Button>
											<Button
												variant="outline"
												size="sm"
												disabled={rowBusy[tag.id]}
												onclick={cancelEdit}
											>
												Cancel
											</Button>
										</div>
									</div>
								{:else}
									<div class="flex items-start justify-between gap-4">
										<div>
											<p class="font-medium text-stone-900">{tag.name}</p>
											<p class="text-xs text-stone-500">/{tag.slug}</p>
										</div>
										<div class="flex shrink-0 gap-2">
											{#if confirmDeleteId === tag.id}
												<Button
													variant="destructive"
													size="sm"
													disabled={rowBusy[tag.id]}
													onclick={() => handleDelete(tag.id)}
												>
													Confirm
												</Button>
												<Button
													variant="outline"
													size="sm"
													disabled={rowBusy[tag.id]}
													onclick={() => (confirmDeleteId = null)}
												>
													Cancel
												</Button>
											{:else}
												<Button variant="outline" size="sm" onclick={() => startEdit(tag)}>
													Edit
												</Button>
												<Button
													variant="outline"
													size="sm"
													onclick={() => (confirmDeleteId = tag.id)}
												>
													Delete
												</Button>
											{/if}
										</div>
									</div>
								{/if}
								{#if rowError[tag.id]}
									<p class="mt-1 text-xs text-destructive">{rowError[tag.id]}</p>
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</Card.Content>
		</Card.Root>

		<Card.Root class="mt-6">
			<Card.Header>
				<Card.Title class="text-sm font-medium text-stone-700">New tag</Card.Title>
			</Card.Header>
			<Card.Content>
				<form class="space-y-4" onsubmit={handleCreate} novalidate>
					<div class="grid gap-4 sm:grid-cols-2">
						<div class="space-y-1.5">
							<Label for="new-tag-name">Name</Label>
							<Input
								id="new-tag-name"
								bind:value={newName}
								aria-invalid={!!createFieldErrors.name}
							/>
							{#if createFieldErrors.name}
								<p class="text-xs text-destructive">{createFieldErrors.name}</p>
							{/if}
						</div>
						<div class="space-y-1.5">
							<Label for="new-tag-slug">Slug (optional)</Label>
							<Input
								id="new-tag-slug"
								bind:value={newSlug}
								aria-invalid={!!createFieldErrors.slug}
							/>
							{#if createFieldErrors.slug}
								<p class="text-xs text-destructive">{createFieldErrors.slug}</p>
							{/if}
						</div>
					</div>

					{#if createError}
						<p class="text-sm text-destructive" role="alert">{createError}</p>
					{/if}

					<Button type="submit" disabled={creating}>
						{creating ? 'Creating…' : 'New tag'}
					</Button>
				</form>
			</Card.Content>
		</Card.Root>

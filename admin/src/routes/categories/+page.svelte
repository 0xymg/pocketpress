<script lang="ts">
	/**
	 * Category management (plan section 9 routes): list, inline create,
	 * inline edit, delete with confirm. Deliberately simple — no post counts
	 * (not available from the API), no pagination (categories are few).
	 */
	import * as Card from '$lib/components/ui/card/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import {
		ApiError,
		createCategory,
		deleteCategory,
		listCategories,
		updateCategory,
		type Category
	} from '$lib/api';

	type LoadState = 'pending' | 'success' | 'error';

	let categories = $state<Category[]>([]);
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	let newName = $state('');
	let newSlug = $state('');
	let newDescription = $state('');
	let creating = $state(false);
	let createError = $state('');
	let createFieldErrors = $state<Record<string, string>>({});

	let editingId = $state<string | null>(null);
	let editName = $state('');
	let editSlug = $state('');
	let editDescription = $state('');
	let editFieldErrors = $state<Record<string, string>>({});
	let rowBusy = $state<Record<string, boolean>>({});
	let rowError = $state<Record<string, string>>({});
	let confirmDeleteId = $state<string | null>(null);

	async function loadCategories() {
		loadState = 'pending';
		try {
			const { categories: fetched } = await listCategories();
			categories = fetched;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load categories.';
			loadState = 'error';
		}
	}

	$effect(() => {
		loadCategories();
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
			const { category } = await createCategory({
				name: newName,
				slug: newSlug || undefined,
				description: newDescription || undefined
			});
			categories = [...categories, category];
			newName = '';
			newSlug = '';
			newDescription = '';
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				createFieldErrors = err.details ?? {};
			}
			createError = describeError(err);
		} finally {
			creating = false;
		}
	}

	function startEdit(category: Category) {
		editingId = category.id;
		editName = category.name;
		editSlug = category.slug;
		editDescription = category.description ?? '';
		editFieldErrors = {};
		rowError = { ...rowError, [category.id]: '' };
	}

	function cancelEdit() {
		editingId = null;
		editFieldErrors = {};
	}

	async function saveEdit(id: string) {
		editFieldErrors = {};
		rowBusy = { ...rowBusy, [id]: true };
		try {
			const { category } = await updateCategory(id, {
				name: editName,
				slug: editSlug,
				description: editDescription || undefined
			});
			categories = categories.map((c) => (c.id === id ? category : c));
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
			await deleteCategory(id);
			categories = categories.filter((c) => c.id !== id);
			confirmDeleteId = null;
		} catch (err) {
			rowError = { ...rowError, [id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [id]: false };
		}
	}
</script>

<header class="mb-8 pt-2">
	<h1 class="text-[32px] font-medium leading-9 text-stone-900">Categories</h1>
	<p class="mt-2 text-[13px] text-stone-500">Group your posts into topics.</p>
</header>

<Card.Root>
			<Card.Header>
				<Card.Title class="text-sm font-medium text-stone-700">All categories</Card.Title>
			</Card.Header>
			<Card.Content>
				{#if loadState === 'pending'}
					<p class="text-sm text-stone-400">Loading categories…</p>
				{:else if loadState === 'error'}
					<p class="text-sm text-destructive">{loadError}</p>
				{:else if categories.length === 0}
					<p class="text-sm text-stone-400">No categories yet — add your first one below.</p>
				{:else}
					<ul class="divide-y divide-stone-100">
						{#each categories as category (category.id)}
							<li class="py-3">
								{#if editingId === category.id}
									<div class="space-y-2">
										<div class="grid gap-2 sm:grid-cols-3">
											<div>
												<Label for="edit-name-{category.id}">Name</Label>
												<Input
													id="edit-name-{category.id}"
													bind:value={editName}
													aria-invalid={!!editFieldErrors.name}
												/>
												{#if editFieldErrors.name}
													<p class="text-xs text-destructive">{editFieldErrors.name}</p>
												{/if}
											</div>
											<div>
												<Label for="edit-slug-{category.id}">Slug</Label>
												<Input
													id="edit-slug-{category.id}"
													bind:value={editSlug}
													aria-invalid={!!editFieldErrors.slug}
												/>
												{#if editFieldErrors.slug}
													<p class="text-xs text-destructive">{editFieldErrors.slug}</p>
												{/if}
											</div>
											<div>
												<Label for="edit-description-{category.id}">Description</Label>
												<Input id="edit-description-{category.id}" bind:value={editDescription} />
											</div>
										</div>
										<div class="flex gap-2">
											<Button
												size="sm"
												disabled={rowBusy[category.id]}
												onclick={() => saveEdit(category.id)}
											>
												{rowBusy[category.id] ? 'Saving…' : 'Save'}
											</Button>
											<Button
												variant="outline"
												size="sm"
												disabled={rowBusy[category.id]}
												onclick={cancelEdit}
											>
												Cancel
											</Button>
										</div>
									</div>
								{:else}
									<div class="flex items-start justify-between gap-4">
										<div>
											<p class="font-medium text-stone-900">{category.name}</p>
											<p class="text-xs text-stone-500">/{category.slug}</p>
											{#if category.description}
												<p class="mt-1 text-sm text-stone-600">{category.description}</p>
											{/if}
										</div>
										<div class="flex shrink-0 gap-2">
											{#if confirmDeleteId === category.id}
												<Button
													variant="destructive"
													size="sm"
													disabled={rowBusy[category.id]}
													onclick={() => handleDelete(category.id)}
												>
													Confirm
												</Button>
												<Button
													variant="outline"
													size="sm"
													disabled={rowBusy[category.id]}
													onclick={() => (confirmDeleteId = null)}
												>
													Cancel
												</Button>
											{:else}
												<Button variant="outline" size="sm" onclick={() => startEdit(category)}>
													Edit
												</Button>
												<Button
													variant="outline"
													size="sm"
													onclick={() => (confirmDeleteId = category.id)}
												>
													Delete
												</Button>
											{/if}
										</div>
									</div>
								{/if}
								{#if rowError[category.id]}
									<p class="mt-1 text-xs text-destructive">{rowError[category.id]}</p>
								{/if}
							</li>
						{/each}
					</ul>
				{/if}
			</Card.Content>
		</Card.Root>

		<Card.Root class="mt-6">
			<Card.Header>
				<Card.Title class="text-sm font-medium text-stone-700">New category</Card.Title>
			</Card.Header>
			<Card.Content>
				<form class="space-y-4" onsubmit={handleCreate} novalidate>
					<div class="grid gap-4 sm:grid-cols-3">
						<div class="space-y-1.5">
							<Label for="new-category-name">Name</Label>
							<Input
								id="new-category-name"
								bind:value={newName}
								aria-invalid={!!createFieldErrors.name}
							/>
							{#if createFieldErrors.name}
								<p class="text-xs text-destructive">{createFieldErrors.name}</p>
							{/if}
						</div>
						<div class="space-y-1.5">
							<Label for="new-category-slug">Slug (optional)</Label>
							<Input
								id="new-category-slug"
								bind:value={newSlug}
								aria-invalid={!!createFieldErrors.slug}
							/>
							{#if createFieldErrors.slug}
								<p class="text-xs text-destructive">{createFieldErrors.slug}</p>
							{/if}
						</div>
						<div class="space-y-1.5">
							<Label for="new-category-description">Description (optional)</Label>
							<Input id="new-category-description" bind:value={newDescription} />
						</div>
					</div>

					{#if createError}
						<p class="text-sm text-destructive" role="alert">{createError}</p>
					{/if}

					<Button type="submit" disabled={creating}>
						{creating ? 'Creating…' : 'New category'}
					</Button>
				</form>
			</Card.Content>
		</Card.Root>

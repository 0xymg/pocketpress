<script lang="ts">
	/**
	 * Content duplication templates (v0.3): list, create by name, delete,
	 * and "Use template" which mints a draft and opens the editor.
	 */
	import { goto } from '$app/navigation';
	import * as Card from '$lib/components/ui/card/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import {
		ApiError,
		createPostFromTemplate,
		createTemplate,
		deleteTemplate,
		listTemplates,
		type PostTemplate
	} from '$lib/api';

	type LoadState = 'pending' | 'success' | 'error';

	let templates = $state<PostTemplate[]>([]);
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	let newName = $state('');
	let newDescription = $state('');
	let newTitle = $state('');
	let creating = $state(false);
	let createError = $state('');
	let createFieldErrors = $state<Record<string, string>>({});

	let rowBusy = $state<Record<string, boolean>>({});
	let rowError = $state<Record<string, string>>({});
	let confirmDeleteId = $state<string | null>(null);
	let usingId = $state<string | null>(null);

	async function loadTemplates() {
		loadState = 'pending';
		try {
			const { templates: fetched } = await listTemplates();
			templates = fetched;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load templates.';
			loadState = 'error';
		}
	}

	$effect(() => {
		loadTemplates();
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
			const { template } = await createTemplate({
				name: newName,
				description: newDescription || undefined,
				title: newTitle || undefined
			});
			templates = [...templates, template].sort((a, b) =>
				a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
			);
			newName = '';
			newDescription = '';
			newTitle = '';
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				createFieldErrors = err.details ?? {};
			}
			createError = describeError(err);
		} finally {
			creating = false;
		}
	}

	async function handleUse(template: PostTemplate) {
		rowError = { ...rowError, [template.id]: '' };
		usingId = template.id;
		try {
			const { post } = await createPostFromTemplate(template.id);
			await goto(`/posts/${post.id}`);
		} catch (err) {
			rowError = { ...rowError, [template.id]: describeError(err) };
			usingId = null;
		}
	}

	async function handleDelete(id: string) {
		rowBusy = { ...rowBusy, [id]: true };
		rowError = { ...rowError, [id]: '' };
		try {
			await deleteTemplate(id);
			templates = templates.filter((t) => t.id !== id);
			confirmDeleteId = null;
		} catch (err) {
			rowError = { ...rowError, [id]: describeError(err) };
		} finally {
			rowBusy = { ...rowBusy, [id]: false };
		}
	}
</script>

<header class="mb-8 pt-2">
	<h1 class="text-[32px] font-medium leading-9 text-stone-900">Templates</h1>
	<p class="mt-2 text-[13px] text-stone-500">
		Reusable starting points for new posts. Use a template to mint a draft with its content and metadata.
	</p>
</header>

<Card.Root>
	<Card.Header>
		<Card.Title class="text-sm font-medium text-stone-700">All templates</Card.Title>
	</Card.Header>
	<Card.Content>
		{#if loadState === 'pending'}
			<p class="text-sm text-stone-400">Loading templates…</p>
		{:else if loadState === 'error'}
			<p class="text-sm text-destructive">{loadError}</p>
		{:else if templates.length === 0}
			<p class="text-sm text-stone-400">No templates yet — add your first one below.</p>
		{:else}
			<ul class="divide-y divide-stone-100">
				{#each templates as template (template.id)}
					<li class="flex flex-wrap items-start justify-between gap-3 py-3">
						<div class="min-w-0 flex-1">
							<p class="text-sm font-medium text-stone-900">{template.name}</p>
							{#if template.description}
								<p class="mt-0.5 text-xs text-stone-500">{template.description}</p>
							{/if}
							<p class="mt-1 text-xs text-stone-400">
								{#if template.title}
									Starts as “{template.title}”
								{:else}
									Starts as “Untitled”
								{/if}
								· {template.categories.length} categories · {template.tags.length} tags
							</p>
							{#if rowError[template.id]}
								<p class="mt-1 text-xs text-destructive">{rowError[template.id]}</p>
							{/if}
						</div>
						<div class="flex flex-wrap items-center gap-2">
							<Button
								size="sm"
								disabled={usingId === template.id || !!rowBusy[template.id]}
								onclick={() => handleUse(template)}
							>
								{usingId === template.id ? 'Creating…' : 'Use template'}
							</Button>
							{#if confirmDeleteId === template.id}
								<span class="text-xs text-stone-600">Delete?</span>
								<Button
									variant="destructive"
									size="sm"
									disabled={!!rowBusy[template.id]}
									onclick={() => handleDelete(template.id)}
								>
									{rowBusy[template.id] ? 'Deleting…' : 'Confirm'}
								</Button>
								<Button
									variant="ghost"
									size="sm"
									disabled={!!rowBusy[template.id]}
									onclick={() => (confirmDeleteId = null)}
								>
									Cancel
								</Button>
							{:else}
								<Button
									variant="ghost"
									size="sm"
									disabled={!!rowBusy[template.id]}
									onclick={() => (confirmDeleteId = template.id)}
								>
									Delete
								</Button>
							{/if}
						</div>
					</li>
				{/each}
			</ul>
		{/if}
	</Card.Content>
</Card.Root>

<Card.Root class="mt-6">
	<Card.Header>
		<Card.Title class="text-sm font-medium text-stone-700">New template</Card.Title>
	</Card.Header>
	<Card.Content>
		<form class="grid max-w-xl gap-4" onsubmit={handleCreate}>
			<div class="grid gap-1.5">
				<Label for="template-name">Name</Label>
				<Input
					id="template-name"
					bind:value={newName}
					aria-invalid={!!createFieldErrors.name}
					required
				/>
				{#if createFieldErrors.name}
					<p class="text-xs text-destructive">{createFieldErrors.name}</p>
				{/if}
			</div>
			<div class="grid gap-1.5">
				<Label for="template-description">Description</Label>
				<Input id="template-description" bind:value={newDescription} />
				{#if createFieldErrors.description}
					<p class="text-xs text-destructive">{createFieldErrors.description}</p>
				{/if}
			</div>
			<div class="grid gap-1.5">
				<Label for="template-title">Starting title</Label>
				<Input
					id="template-title"
					bind:value={newTitle}
					placeholder="Optional — e.g. Changelog: "
					aria-invalid={!!createFieldErrors.title}
				/>
				{#if createFieldErrors.title}
					<p class="text-xs text-destructive">{createFieldErrors.title}</p>
				{/if}
			</div>
			{#if createError}
				<p class="text-sm text-destructive">{createError}</p>
			{/if}
			<div>
				<Button type="submit" disabled={creating}>
					{creating ? 'Creating…' : 'New template'}
				</Button>
			</div>
		</form>
	</Card.Content>
</Card.Root>

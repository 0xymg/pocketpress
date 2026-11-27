<script lang="ts">
	/**
	 * Shared editor for /posts/new and /posts/[id] (plan section 9 — editor
	 * screen). Both routes are thin wrappers around this component: /posts/new
	 * renders it with no `postId` (a blank, new-post form); /posts/[id] passes
	 * the route param.
	 *
	 * Content is a Lexical rich-text editor (Phase 4): the canonical document is
	 * stored as content_json, and content_html (server-re-sanitized) rides along.
	 *
	 * Status (Phase 6 — plan sections 7 and 9) is driven entirely by the
	 * contextual action bar below, never edited directly: draft/scheduled/
	 * published/archived each show their own action set, and the current
	 * status is surfaced as a read-only Badge. Content changes (Save) are
	 * always separate from status changes (Publish/Schedule/Unpublish/
	 * Archive) — saving never publishes, and a status action is blocked while
	 * the form has unsaved edits.
	 */
	import { untrack } from 'svelte';
	import { beforeNavigate, goto } from '$app/navigation';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import { Textarea } from '$lib/components/ui/textarea/index.js';
	import { auth } from '$lib/auth.svelte';
	import { Popup, popupItemClass } from '$lib/components/ui/popup/index.js';
	import {
		ApiError,
		approveReview,
		archivePost,
		createPost,
		createPreviewToken,
		deletePost,
		duplicatePost,
		getPost,
		getSettings,
		listAuthors,
		listCategories,
		listTags,
		publishPost,
		requestChanges,
		schedulePost,
		submitReview,
		unpublishPost,
		updatePost,
		withdrawReview,
		type Author,
		type Category,
		type PostFull,
		type Tag
	} from '$lib/api';
	import { PostEditorState } from '$lib/post-editor-state.svelte';
	import { mediaPicker } from '$lib/media-picker-state.svelte';
	import MediaPicker from '$lib/components/media/MediaPicker.svelte';
	import LexicalEditor, { type InsertedImage } from './LexicalEditor.svelte';
	import {
		clearDraft,
		draftKey,
		readDraft,
		shouldRestoreDraft,
		writeDraft,
		type DraftForm,
		type DraftSnapshot
	} from '$lib/post-draft';

	let { postId }: { postId?: string } = $props();

	const formState = new PostEditorState();

	type LoadState = 'pending' | 'success' | 'error';
	// untrack: this only needs postId's *initial* value, to avoid a one-frame
	// flash of the blank new-post form before the load effect below flips it
	// to 'pending'. It's intentionally not reactive to later postId changes.
	let loadState = $state<LoadState>(untrack(() => (postId ? 'pending' : 'success')));
	let loadError = $state('');

	let categories = $state<Category[]>([]);
	let tags = $state<Tag[]>([]);
	let authors = $state<Author[]>([]);
	let availableLocales = $state<string[]>(['en']);

	let saving = $state(false);
	let saveError = $state('');
	let fieldErrors = $state<Record<string, string>>({});
	let savedAt = $state<Date | null>(null);

	let deleting = $state(false);
	let confirmingDelete = $state(false);
	let deleteError = $state('');

	// --- Status actions: publish/unpublish/schedule/archive (Phase 6) --------
	// One shared busy/error pair for all of them — only one can run at a time
	// since they're all behind the same disabled-while-dirty action bar.
	let statusBusy = $state(false);
	let statusError = $state('');
	let publishConfirming = $state(false);
	let schedulePanelOpen = $state(false);
	let scheduleValue = $state('');
	let scheduleFieldError = $state('');
	let previewBusy = $state(false);
	let previewError = $state('');
	let previewHintVisible = $state(false);

	// --- Draft review (v0.3) -------------------------------------------------
	let changesPanelOpen = $state(false);
	let changesNote = $state('');
	let changesFieldError = $state('');
	let requireReviewBeforePublish = $state(false);

	/** Pads a datetime-local value's date parts, local time (no timezone conversion). */
	function toLocalDatetimeInputValue(iso: string): string {
		const date = new Date(iso);
		const pad = (n: number) => String(n).padStart(2, '0');
		return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
	}

	/** Default schedule panel value: now + 1 hour, local time. */
	function defaultScheduleValue(): string {
		return toLocalDatetimeInputValue(new Date(Date.now() + 60 * 60 * 1000).toISOString());
	}

	/** `min` attribute for the schedule datetime-local input: now, local time. */
	function nowDatetimeInputValue(): string {
		return toLocalDatetimeInputValue(new Date().toISOString());
	}

	/** `<input type="datetime-local">` values are local-time, no offset — the Date constructor parses them as local, so this just re-serializes to RFC3339 UTC. */
	function localDatetimeToRfc3339(value: string): string {
		return new Date(value).toISOString();
	}

	function formatLocalDateTime(iso: string): string {
		return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
	}

	/** Status actions are blocked while the form has unsaved edits — "Save your changes first" (plan brief). Returns false and sets statusError when blocked. */
	function requireClean(): boolean {
		if (formState.dirty) {
			statusError = 'Save your changes first.';
			return false;
		}
		statusError = '';
		return true;
	}

	async function runStatusOp(op: () => Promise<{ post: PostFull }>) {
		if (!requireClean()) return;
		statusError = '';
		statusBusy = true;
		try {
			const { post } = await op();
			formState.markSaved(post);
			loadedUpdatedAt = post.updated_at;
			publishConfirming = false;
			schedulePanelOpen = false;
		} catch (err) {
			statusError = describeSaveError(err);
		} finally {
			statusBusy = false;
		}
	}

	function openPublishConfirm() {
		if (!requireClean()) return;
		publishConfirming = true;
	}

	function cancelPublishConfirm() {
		publishConfirming = false;
	}

	async function confirmPublish() {
		if (!postId) return;
		await runStatusOp(() => publishPost(postId));
	}

	async function handlePublishNow() {
		if (!postId) return;
		await runStatusOp(() => publishPost(postId));
	}

	async function handleUnpublish() {
		if (!postId) return;
		await runStatusOp(() => unpublishPost(postId));
	}

	async function handleArchive() {
		if (!postId) return;
		await runStatusOp(() => archivePost(postId));
	}

	async function handleSubmitReview() {
		if (!postId) return;
		await runStatusOp(() => submitReview(postId));
	}

	async function handleApproveReview() {
		if (!postId) return;
		await runStatusOp(() => approveReview(postId));
	}

	async function handleWithdrawReview() {
		if (!postId) return;
		await runStatusOp(() => withdrawReview(postId));
	}

	function openChangesPanel() {
		if (!requireClean()) return;
		changesNote = '';
		changesFieldError = '';
		changesPanelOpen = true;
	}

	function cancelChangesPanel() {
		changesPanelOpen = false;
		changesFieldError = '';
	}

	async function confirmRequestChanges() {
		if (!postId) return;
		const note = changesNote.trim();
		if (!note) {
			changesFieldError = 'A note is required.';
			return;
		}
		statusError = '';
		statusBusy = true;
		try {
			const { post } = await requestChanges(postId, { note });
			formState.markSaved(post);
			loadedUpdatedAt = post.updated_at;
			changesPanelOpen = false;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed' && err.details?.note) {
				changesFieldError = err.details.note;
			} else {
				statusError = describeSaveError(err);
			}
		} finally {
			statusBusy = false;
		}
	}

	// Duplicate (from the "..." menu) — copies the last-saved post and opens the
	// copy, mirroring the posts list's duplicate action.
	let duplicating = $state(false);
	async function handleDuplicate() {
		if (!postId) return;
		statusError = '';
		duplicating = true;
		try {
			const { post } = await duplicatePost(postId);
			await goto(`/posts/${post.id}`);
		} catch (err) {
			statusError = describeSaveError(err);
		} finally {
			duplicating = false;
		}
	}

	function openSchedulePanel() {
		if (!requireClean()) return;
		scheduleValue =
			formState.status === 'scheduled' && formState.scheduledFor
				? toLocalDatetimeInputValue(formState.scheduledFor)
				: defaultScheduleValue();
		scheduleFieldError = '';
		schedulePanelOpen = true;
	}

	function cancelSchedulePanel() {
		schedulePanelOpen = false;
		scheduleFieldError = '';
	}

	async function confirmSchedule() {
		if (!postId) return;
		if (!requireClean()) return;
		scheduleFieldError = '';
		if (!scheduleValue) {
			scheduleFieldError = 'Choose a date and time.';
			return;
		}
		const iso = localDatetimeToRfc3339(scheduleValue);
		if (new Date(iso).getTime() <= Date.now()) {
			scheduleFieldError = 'Scheduled time must be in the future.';
			return;
		}
		statusError = '';
		statusBusy = true;
		try {
			const { post } = await schedulePost(postId, { scheduled_for: iso });
			formState.markSaved(post);
			loadedUpdatedAt = post.updated_at;
			schedulePanelOpen = false;
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed' && err.details?.scheduled_for) {
				scheduleFieldError = err.details.scheduled_for;
			} else {
				statusError = describeSaveError(err);
			}
		} finally {
			statusBusy = false;
		}
	}

	async function handlePreview() {
		if (!postId) return;
		previewError = '';
		previewHintVisible = false;
		previewBusy = true;
		try {
			const result = await createPreviewToken(postId);
			if (result.preview_url) {
				window.open(result.preview_url, '_blank');
			} else {
				previewHintVisible = true;
			}
		} catch (err) {
			previewError = describeSaveError(err);
		} finally {
			previewBusy = false;
		}
	}

	// --- Rich-text editor + local autosave (plan section 9) ------------------
	// The editor mounts once the form is ready to show (new post, or after an
	// existing post loads). `editorReloadToken` forces a remount when we replace
	// the document wholesale (i.e. restoring a local draft). `firstReady`
	// distinguishes the initial mount (which sets the dirty baseline) from a
	// post-restore remount (which must keep the form dirty).
	let editorReady = $state(false);
	let firstReady = true;
	let editorReloadToken = $state(0);
	let loadedUpdatedAt: string | null = null;
	let restoreCandidate = $state<DraftSnapshot | null>(null);
	let autosaveTimer: ReturnType<typeof setTimeout> | undefined;

	function hasLocalStorage(): boolean {
		return typeof localStorage !== 'undefined';
	}

	function currentForm(): DraftForm {
		return {
			title: formState.title,
			slug: formState.slug,
			excerpt: formState.excerpt,
			contentJson: formState.contentJson,
			contentHtml: formState.contentHtml,
			status: formState.status,
			authorId: formState.authorId,
			categoryIds: [...formState.categoryIds],
			tagIds: [...formState.tagIds],
			featured: formState.featured,
			seoTitle: formState.seoTitle,
			seoDescription: formState.seoDescription,
			canonicalUrl: formState.canonicalUrl
		};
	}

	function maybeOfferRestore() {
		if (!hasLocalStorage()) return;
		const local = readDraft(localStorage, draftKey(postId));
		const server = formState.isNew ? null : currentForm();
		restoreCandidate = shouldRestoreDraft({ local, server, serverUpdatedAt: loadedUpdatedAt })
			? local
			: null;
	}

	function handleEditorReady(json: string, html: string) {
		if (firstReady) {
			firstReady = false;
			// Fold the editor's own canonical serialization into the baseline so
			// the untouched document doesn't read as dirty on load.
			formState.commitInitialContent(json, html);
			maybeOfferRestore();
			editorReady = true;
		} else {
			// Remount after a restore: the restored values are already in place;
			// just sync the editor's canonical output without re-baselining.
			formState.setContent(json, html);
		}
	}

	function handleEditorChange(json: string, html: string) {
		formState.setContent(json, html);
	}

	function restoreLocalDraft() {
		const draft = restoreCandidate;
		if (!draft) return;
		formState.title = draft.title;
		formState.slug = draft.slug;
		formState.excerpt = draft.excerpt;
		formState.status = draft.status;
		formState.authorId = draft.authorId;
		formState.categoryIds = [...draft.categoryIds];
		formState.tagIds = [...draft.tagIds];
		formState.featured = draft.featured;
		formState.seoTitle = draft.seoTitle;
		formState.seoDescription = draft.seoDescription;
		formState.canonicalUrl = draft.canonicalUrl;
		formState.contentJson = draft.contentJson;
		formState.contentHtml = draft.contentHtml;
		formState.slugTouched = true;
		restoreCandidate = null;
		editorReloadToken += 1;
	}

	function discardLocalDraft() {
		if (hasLocalStorage()) clearDraft(localStorage, draftKey(postId));
		restoreCandidate = null;
	}

	// Debounced (~2s) local snapshot of the whole form for crash protection.
	// Writes/clears localStorage only (never reactive state), so there's no
	// $effect feedback loop; currentForm() is read untracked since formState.dirty
	// above already registers every field as a dependency.
	$effect(() => {
		const dirty = formState.dirty;
		if (!editorReady) return;
		const form = untrack(() => currentForm());
		clearTimeout(autosaveTimer);
		if (!dirty) {
			if (hasLocalStorage()) clearDraft(localStorage, draftKey(postId));
			return;
		}
		autosaveTimer = setTimeout(() => {
			if (!hasLocalStorage()) return;
			const snapshot: DraftSnapshot = { ...form, savedAt: new Date().toISOString() };
			writeDraft(localStorage, draftKey(postId), snapshot);
		}, 2000);
		return () => clearTimeout(autosaveTimer);
	});

	async function loadReferenceData() {
		try {
			const [{ categories: cats }, { tags: allTags }, { authors: allAuthors }] =
				await Promise.all([listCategories(), listTags(), listAuthors()]);
			categories = cats;
			tags = allTags;
			authors = allAuthors;
		} catch {
			// Reference lists are best-effort — the pickers just render empty
			// and the user can still save with no categories/tags selected.
		}
		try {
			const { settings } = await getSettings();
			if (settings.available_locales?.length) {
				availableLocales = settings.available_locales;
			}
			requireReviewBeforePublish = !!settings.require_review_before_publish;
			// Seed a new post's locale from the site default without dirtying the form.
			if (formState.isNew && settings.locale && formState.locale === 'en') {
				formState.locale = settings.locale;
			}
		} catch {
			// Settings read failed — keep locale/review defaults.
		}
	}

	async function loadPost(id: string) {
		loadState = 'pending';
		// Re-arm the editor so its next mount re-baselines against this post.
		firstReady = true;
		editorReady = false;
		restoreCandidate = null;
		loadedUpdatedAt = null;
		try {
			const { post } = await getPost(id);
			formState.loadFromPost(post);
			loadedUpdatedAt = post.updated_at;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load post.';
			loadState = 'error';
		}
	}

	$effect(() => {
		loadReferenceData();
	});

	$effect(() => {
		const currentPostId = postId;
		const defaultAuthorId = auth.user?.id ?? '';
		if (currentPostId) {
			loadPost(currentPostId);
		} else {
			// resetForNew() both reads and writes formState's own $state fields
			// (via snapshot(), to set the new dirty baseline). Left untracked,
			// that read would register as a dependency of *this* effect too —
			// and since the effect also just wrote those same fields, it would
			// immediately re-run itself, forever. untrack() runs the reset
			// without registering those internal reads as dependencies, so
			// this effect only re-fires when postId or the default author
			// actually change.
			untrack(() => {
				firstReady = true;
				editorReady = false;
				restoreCandidate = null;
				loadedUpdatedAt = null;
				formState.resetForNew(defaultAuthorId);
			});
		}
	});

	function toggleCategory(id: string) {
		formState.categoryIds = formState.categoryIds.includes(id)
			? formState.categoryIds.filter((c) => c !== id)
			: [...formState.categoryIds, id];
	}

	function toggleTag(id: string) {
		formState.tagIds = formState.tagIds.includes(id)
			? formState.tagIds.filter((t) => t !== id)
			: [...formState.tagIds, id];
	}

	// --- Cover/OG image + inline "insert image" (Phase 4 media browser) ------
	// All three share the one <MediaPicker /> instance below via the
	// mediaPicker singleton — only one can be open at a time anyway.

	async function pickCoverImage() {
		const media = await mediaPicker.pick();
		if (media) formState.coverImage = media;
	}

	function clearCoverImage() {
		formState.coverImage = null;
	}

	async function pickOgImage() {
		const media = await mediaPicker.pick();
		if (media) formState.ogImage = media;
	}

	function clearOgImage() {
		formState.ogImage = null;
	}

	/** Passed as LexicalEditor's onInsertImage — opens the same picker instead of the default URL prompt. */
	async function insertImageFromMediaLibrary(): Promise<InsertedImage | null> {
		const media = await mediaPicker.pick();
		if (!media) return null;
		return { src: media.url, alt: media.alt };
	}

	function formatSavedAt(date: Date): string {
		const hh = String(date.getHours()).padStart(2, '0');
		const mm = String(date.getMinutes()).padStart(2, '0');
		return `Saved ${hh}:${mm}`;
	}

	function describeSaveError(err: unknown): string {
		if (err instanceof ApiError) return err.message;
		return err instanceof Error ? err.message : 'Unable to save post.';
	}

	async function handleSave() {
		saveError = '';
		fieldErrors = {};
		saving = true;
		try {
			const payload = formState.toPayload();
			if (formState.isNew) {
				const { post } = await createPost(payload);
				// Clear the "new" draft before the route swaps to /posts/:id.
				if (hasLocalStorage()) clearDraft(localStorage, draftKey(postId));
				formState.markSaved(post);
				loadedUpdatedAt = post.updated_at;
				savedAt = new Date();
				await goto(`/posts/${post.id}`, { replaceState: true });
			} else if (postId) {
				const { post } = await updatePost(postId, payload);
				formState.markSaved(post);
				loadedUpdatedAt = post.updated_at;
				if (hasLocalStorage()) clearDraft(localStorage, draftKey(postId));
				savedAt = new Date();
			}
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed') {
				fieldErrors = err.details ?? {};
			}
			saveError = describeSaveError(err);
		} finally {
			saving = false;
		}
	}

	async function handleDelete() {
		if (!postId) return;
		deleteError = '';
		deleting = true;
		try {
			await deletePost(postId);
			await goto('/posts');
		} catch (err) {
			deleteError = describeSaveError(err);
			deleting = false;
			confirmingDelete = false;
		}
	}

	// Unsaved-changes guard (plan section 9): warn before an in-app
	// navigation and before a full page unload while the form is dirty.
	beforeNavigate((navigation) => {
		if (!formState.dirty) return;
		const confirmed = confirm('You have unsaved changes. Leave without saving?');
		if (!confirmed) navigation.cancel();
	});

	function handleBeforeUnload(event: BeforeUnloadEvent) {
		if (!formState.dirty) return;
		event.preventDefault();
		event.returnValue = '';
	}

	$effect(() => {
		window.addEventListener('beforeunload', handleBeforeUnload);
		return () => window.removeEventListener('beforeunload', handleBeforeUnload);
	});
</script>

{#if postId && loadState === 'pending'}
	<div class="flex min-h-screen items-center justify-center bg-white">
		<p class="text-[13px] text-stone-400">Loading post…</p>
	</div>
{:else if postId && loadState === 'error'}
	<div class="flex min-h-screen items-center justify-center bg-white">
		<p class="text-[13px] text-destructive">{loadError}</p>
	</div>
{:else}
	<div class="min-h-screen bg-white pb-16">
		<!-- Document header: breadcrumb (step-nav) + borderless title (Payload doc-header) -->
		<div class="px-6 pt-2 lg:px-[60px]">
			<nav aria-label="Breadcrumb" class="flex items-center gap-2 text-[13px]">
				<a href="/posts" class="text-stone-500 no-underline hover:text-stone-900">Posts</a>
				<span class="text-stone-300">/</span>
				<span class="text-stone-800">{formState.isNew ? 'Create New' : formState.title || 'Edit'}</span>
			</nav>
			<h1 class="mt-3 text-[32px] font-medium leading-9 text-stone-900">
				{formState.title || (formState.isNew ? 'Create New' : 'Untitled')}
			</h1>
		</div>

		<!-- Doc controls: meta (left) + actions (right), sticky with a bottom divider -->
		<div class="sticky top-0 z-20 mt-4 border-b border-stone-100 bg-white/80 backdrop-blur">
			<div class="flex flex-wrap items-center justify-end gap-2 px-6 py-3 lg:px-[60px]">
				{#if savedAt}
					<span class="mr-auto text-xs text-stone-400">{formatSavedAt(savedAt)}</span>
				{/if}

				{#if !formState.isNew && formState.status !== 'archived'}
					<Button variant="outline" size="sm" disabled={previewBusy} onclick={handlePreview}>
						{previewBusy ? 'Preparing…' : 'Preview'}
					</Button>
				{/if}

				{#if !formState.isNew && formState.status === 'draft'}
					{#if publishConfirming}
						<span class="text-xs text-stone-600">Publish now?</span>
						<Button variant="primary" size="sm" disabled={statusBusy} onclick={confirmPublish}>
							{statusBusy ? 'Publishing…' : 'Confirm'}
						</Button>
						<Button variant="outline" size="sm" disabled={statusBusy} onclick={cancelPublishConfirm}>
							Cancel
						</Button>
					{:else}
						<Button
							variant="primary"
							size="sm"
							disabled={formState.dirty || statusBusy}
							onclick={openPublishConfirm}
						>
							Publish
						</Button>
					{/if}
				{/if}

				{#if formState.status === 'archived'}
					<Button variant="primary" size="sm" disabled={statusBusy} onclick={handleUnpublish}>
						{statusBusy ? 'Restoring…' : 'Restore to draft'}
					</Button>
				{:else}
					<Button size="sm" disabled={saving} onclick={handleSave}>
						{saving ? 'Saving…' : 'Save'}
					</Button>
				{/if}

				{#if !formState.isNew && (formState.status === 'draft' || formState.status === 'archived')}
					{#if formState.reviewStatus === 'none' || formState.reviewStatus === 'changes_requested'}
						<Button
							variant="outline"
							size="sm"
							disabled={formState.dirty || statusBusy}
							onclick={handleSubmitReview}
						>
							{statusBusy ? 'Submitting…' : 'Submit for review'}
						</Button>
					{:else if formState.reviewStatus === 'pending'}
						<Button
							variant="primary"
							size="sm"
							disabled={formState.dirty || statusBusy}
							onclick={handleApproveReview}
						>
							{statusBusy ? 'Approving…' : 'Approve'}
						</Button>
						<Button
							variant="outline"
							size="sm"
							disabled={formState.dirty || statusBusy}
							onclick={openChangesPanel}
						>
							Request changes
						</Button>
						<Button
							variant="outline"
							size="sm"
							disabled={formState.dirty || statusBusy}
							onclick={handleWithdrawReview}
						>
							Withdraw
						</Button>
					{/if}
				{/if}

				{#if !formState.isNew}
					<!-- Secondary actions in a Payload-style "..." popup menu -->
					<Popup align="right">
						{#snippet trigger(toggle, open)}
							<button
								type="button"
								aria-haspopup="menu"
								aria-expanded={open}
								aria-label="More actions"
								class="flex h-8 w-8 flex-col items-center justify-center gap-[3px] rounded-[4px] border border-stone-200 text-stone-600 hover:bg-stone-50"
								onclick={toggle}
							>
								<span class="h-[3px] w-[3px] rounded-full bg-current"></span>
								<span class="h-[3px] w-[3px] rounded-full bg-current"></span>
								<span class="h-[3px] w-[3px] rounded-full bg-current"></span>
							</button>
						{/snippet}
						{#snippet children(close)}
							{#if formState.status === 'draft'}
								<button type="button" class={popupItemClass} disabled={formState.dirty || statusBusy} onclick={() => { close(); openSchedulePanel(); }}>Schedule…</button>
								<button type="button" class={popupItemClass} disabled={formState.dirty || statusBusy} onclick={() => { close(); handleArchive(); }}>Archive</button>
							{:else if formState.status === 'scheduled'}
								<button type="button" class={popupItemClass} disabled={formState.dirty || statusBusy} onclick={() => { close(); handlePublishNow(); }}>Publish now</button>
								<button type="button" class={popupItemClass} disabled={formState.dirty || statusBusy} onclick={() => { close(); openSchedulePanel(); }}>Edit schedule…</button>
								<button type="button" class={popupItemClass} disabled={formState.dirty || statusBusy} onclick={() => { close(); handleUnpublish(); }}>Back to draft</button>
								<button type="button" class={popupItemClass} disabled={formState.dirty || statusBusy} onclick={() => { close(); handleArchive(); }}>Archive</button>
							{:else if formState.status === 'published'}
								<button type="button" class={popupItemClass} disabled={formState.dirty || statusBusy} onclick={() => { close(); handleUnpublish(); }}>Unpublish</button>
								<button type="button" class={popupItemClass} disabled={formState.dirty || statusBusy} onclick={() => { close(); handleArchive(); }}>Archive</button>
							{/if}
							<button type="button" class={popupItemClass} disabled={duplicating} onclick={() => { close(); handleDuplicate(); }}>Duplicate</button>
							<button type="button" class={popupItemClass} onclick={() => { close(); confirmingDelete = true; }}>Delete</button>
						{/snippet}
					</Popup>
				{/if}
			</div>

			{#if confirmingDelete}
				<div class="flex flex-wrap items-center gap-2 px-6 pb-2 text-xs lg:px-[60px]">
					<span class="text-stone-600">Delete this post permanently?</span>
					<Button variant="destructive" size="sm" disabled={deleting} onclick={handleDelete}>
						{deleting ? 'Deleting…' : 'Confirm delete'}
					</Button>
					<Button variant="outline" size="sm" disabled={deleting} onclick={() => (confirmingDelete = false)}>
						Cancel
					</Button>
				</div>
			{/if}

			{#if formState.dirty && !formState.isNew && formState.status !== 'archived'}
				<p class="px-6 lg:px-[60px] pb-2 text-xs text-stone-400">
					Save your changes first to publish, schedule, unpublish or archive.
				</p>
			{/if}
			{#if changesPanelOpen}
				<div class="px-6 lg:px-[60px] pb-3">
					<div class="space-y-2 rounded-md border border-stone-200 bg-stone-50 p-3">
						<Label for="changes-note">Request changes</Label>
						<Textarea id="changes-note" class="min-h-20" bind:value={changesNote} />
						{#if changesFieldError}
							<p class="text-xs text-destructive">{changesFieldError}</p>
						{/if}
						<div class="flex flex-wrap gap-2">
							<Button size="sm" disabled={statusBusy} onclick={confirmRequestChanges}>
								{statusBusy ? 'Sending…' : 'Send'}
							</Button>
							<Button variant="outline" size="sm" disabled={statusBusy} onclick={cancelChangesPanel}>
								Cancel
							</Button>
						</div>
					</div>
				</div>
			{/if}
			{#if statusError}
				<p class="px-6 lg:px-[60px] pb-2 text-xs text-destructive" role="alert">
					{statusError}
				</p>
			{/if}
			{#if deleteError}
				<p class="px-6 lg:px-[60px] pb-2 text-xs text-destructive">{deleteError}</p>
			{/if}
			{#if previewError}
				<p class="px-6 lg:px-[60px] pb-2 text-xs text-destructive">{previewError}</p>
			{/if}
			{#if previewHintVisible}
				<p class="px-6 lg:px-[60px] pb-2 text-xs text-stone-500">
					Set a preview URL template in
					<a href="/settings/general" class="text-primary underline">Settings → General</a>.
				</p>
			{/if}

			{#if schedulePanelOpen}
				<div class="px-6 lg:px-[60px] pb-3">
					<div
						class="flex flex-wrap items-end gap-3 rounded-md border border-stone-200 bg-stone-50 p-3"
					>
						<div class="space-y-1.5">
							<Label for="schedule-datetime">Scheduled for</Label>
							<input
								id="schedule-datetime"
								type="datetime-local"
								class="border-input h-9 rounded-md border bg-white px-3 py-1 text-sm shadow-xs"
								bind:value={scheduleValue}
								min={nowDatetimeInputValue()}
							/>
							{#if scheduleFieldError}
								<p class="text-xs text-destructive">{scheduleFieldError}</p>
							{/if}
						</div>
						<Button size="sm" disabled={statusBusy} onclick={confirmSchedule}>
							{statusBusy ? 'Scheduling…' : 'Confirm'}
						</Button>
						<Button variant="outline" size="sm" disabled={statusBusy} onclick={cancelSchedulePanel}>
							Cancel
						</Button>
					</div>
				</div>
			{/if}
		</div>

		<!-- Fields: main column + sticky right rail, split by Payload's 1px divider -->
		<div class="lg:flex lg:items-start">
			<div class="min-w-0 flex-1 space-y-6 px-6 pt-8 pb-10 lg:border-r lg:border-stone-100 lg:px-[60px]">
				{#if restoreCandidate}
					<div
						class="flex flex-wrap items-center justify-between gap-3 rounded-md border border-teal-200 bg-teal-50 px-4 py-3"
						role="status"
					>
						<p class="text-sm text-teal-900">
							Unsaved local draft found — restore it or discard it.
						</p>
						<div class="flex items-center gap-2">
							<Button size="sm" onclick={restoreLocalDraft}>Restore</Button>
							<Button variant="outline" size="sm" onclick={discardLocalDraft}>Discard</Button>
						</div>
					</div>
				{/if}

				{#if saveError}
					<p class="text-sm text-destructive" role="alert">{saveError}</p>
				{/if}

				<div class="space-y-1.5">
					<Label for="post-title">Title</Label>
					<Input
						id="post-title"
						placeholder="Post title"
						value={formState.title}
						oninput={(event) => formState.setTitle(event.currentTarget.value)}
						aria-invalid={!!fieldErrors.title}
					/>
					{#if fieldErrors.title}
						<p class="text-xs text-destructive">{fieldErrors.title}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<span class="text-[13px] text-stone-500">Content</span>
					{#key `${postId ?? 'new'}-${editorReloadToken}`}
						<LexicalEditor
							initialJson={formState.contentJson}
							onReady={handleEditorReady}
							onChange={handleEditorChange}
							onInsertImage={insertImageFromMediaLibrary}
							invalid={!!fieldErrors.content_json || !!fieldErrors.content_html}
						/>
					{/key}
					{#if fieldErrors.content_json || fieldErrors.content_html}
						<p class="text-xs text-destructive">
							{fieldErrors.content_json ?? fieldErrors.content_html}
						</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label for="post-excerpt">Excerpt</Label>
					<Textarea
						id="post-excerpt"
						class="min-h-24"
						bind:value={formState.excerpt}
						aria-invalid={!!fieldErrors.excerpt}
					/>
					{#if fieldErrors.excerpt}
						<p class="text-xs text-destructive">{fieldErrors.excerpt}</p>
					{/if}
				</div>
			</div>

			<aside
				class="w-full shrink-0 space-y-6 px-6 pt-8 pb-10 lg:sticky lg:top-14 lg:w-[340px] lg:self-start lg:px-10"
			>
				{#if !formState.isNew}
					<!-- Payload-style read-only meta rows: muted label + weight-600 value -->
					<div class="space-y-1.5 border-b border-stone-100 pb-5 text-[13px]">
						<div class="flex items-baseline justify-between gap-3">
							<span class="text-stone-400">Status</span>
							<span class="font-semibold capitalize text-stone-800">{formState.status}</span>
						</div>
						<div class="flex items-baseline justify-between gap-3">
							<span class="text-stone-400">Review</span>
							<span class="font-semibold capitalize text-stone-800"
								>{formState.reviewStatus.replaceAll('_', ' ')}</span
							>
						</div>
						{#if formState.reviewNote}
							<div class="space-y-1">
								<span class="text-stone-400">Review note</span>
								<p class="text-stone-800">{formState.reviewNote}</p>
							</div>
						{/if}
						{#if requireReviewBeforePublish && formState.reviewStatus !== 'approved' && (formState.status === 'draft' || formState.status === 'archived')}
							<p class="text-[12px] text-amber-700">
								Approval required before publish or schedule.
							</p>
						{/if}
						{#if formState.status === 'scheduled' && formState.scheduledFor}
							<div class="flex items-baseline justify-between gap-3">
								<span class="text-stone-400">Scheduled for</span>
								<span class="font-semibold text-stone-800">{formatLocalDateTime(formState.scheduledFor)}</span>
							</div>
						{/if}
						{#if formState.publishedAt}
							<div class="flex items-baseline justify-between gap-3">
								<span class="text-stone-400">Published</span>
								<span class="font-semibold text-stone-800">{formatLocalDateTime(formState.publishedAt)}</span>
							</div>
						{/if}
						{#if formState.updatedAt}
							<div class="flex items-baseline justify-between gap-3">
								<span class="text-stone-400">Last Modified</span>
								<span class="font-semibold text-stone-800">{formatLocalDateTime(formState.updatedAt)}</span>
							</div>
						{/if}
						{#if formState.createdAt}
							<div class="flex items-baseline justify-between gap-3">
								<span class="text-stone-400">Created</span>
								<span class="font-semibold text-stone-800">{formatLocalDateTime(formState.createdAt)}</span>
							</div>
						{/if}
					</div>
				{/if}

				<div class="space-y-1.5">
					<Label for="post-slug">Slug</Label>
					<Input
						id="post-slug"
						value={formState.slug}
						oninput={(event) => formState.setSlug(event.currentTarget.value)}
						aria-invalid={!!fieldErrors.slug}
					/>
					{#if fieldErrors.slug}
						<p class="text-xs text-destructive">{fieldErrors.slug}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label for="post-author">Author</Label>
					<select
						id="post-author"
						class="h-10 w-full rounded-[3px] border border-stone-200 bg-white px-3 text-[13px] text-stone-900 outline-none hover:border-stone-300"
						bind:value={formState.authorId}
					>
						{#each authors as authorOption (authorOption.id)}
							<option value={authorOption.id}>{authorOption.name}</option>
						{/each}
					</select>
					{#if fieldErrors.author}
						<p class="text-xs text-destructive">{fieldErrors.author}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label for="post-locale">Locale</Label>
					<select
						id="post-locale"
						class="h-10 w-full rounded-[3px] border border-stone-200 bg-white px-3 text-[13px] text-stone-900 outline-none hover:border-stone-300"
						bind:value={formState.locale}
						aria-invalid={!!fieldErrors.locale}
					>
						{#if !availableLocales.includes(formState.locale) && formState.locale}
							<option value={formState.locale}>{formState.locale}</option>
						{/if}
						{#each availableLocales as loc (loc)}
							<option value={loc}>{loc}</option>
						{/each}
					</select>
					{#if fieldErrors.locale}
						<p class="text-xs text-destructive">{fieldErrors.locale}</p>
					{/if}
				</div>

				<div class="space-y-1.5">
					<Label for="post-translate-of">Translation of (post id)</Label>
					<Input
						id="post-translate-of"
						placeholder="Optional — link as translation"
						bind:value={formState.translateOf}
						aria-invalid={!!fieldErrors.translate_of}
					/>
					{#if fieldErrors.translate_of}
						<p class="text-xs text-destructive">{fieldErrors.translate_of}</p>
					{/if}
				</div>

				<div class="space-y-2">
					<span class="text-[13px] text-stone-500">Categories</span>
					{#if categories.length === 0}
						<p class="text-xs text-stone-400">No categories yet.</p>
					{/if}
					{#each categories as category (category.id)}
						<label class="flex items-center gap-2 text-[13px] text-stone-700">
							<input
								type="checkbox"
								checked={formState.categoryIds.includes(category.id)}
								onchange={() => toggleCategory(category.id)}
							/>
							{category.name}
						</label>
					{/each}
					{#if fieldErrors.categories}
						<p class="text-xs text-destructive">{fieldErrors.categories}</p>
					{/if}
				</div>

				<div class="space-y-2">
					<span class="text-[13px] text-stone-500">Tags</span>
					{#if tags.length === 0}
						<p class="text-xs text-stone-400">No tags yet.</p>
					{/if}
					{#each tags as tag (tag.id)}
						<label class="flex items-center gap-2 text-[13px] text-stone-700">
							<input
								type="checkbox"
								checked={formState.tagIds.includes(tag.id)}
								onchange={() => toggleTag(tag.id)}
							/>
							{tag.name}
						</label>
					{/each}
					{#if fieldErrors.tags}
						<p class="text-xs text-destructive">{fieldErrors.tags}</p>
					{/if}
				</div>

				<div class="space-y-2">
					<span class="text-[13px] text-stone-500">Cover image</span>
					{#if formState.coverImage}
						<div class="flex items-center gap-3 rounded-[4px] border border-stone-200 p-2">
							<img
								src={formState.coverImage.url}
								alt={formState.coverImage.alt}
								class="h-11 w-11 shrink-0 rounded-[3px] border border-stone-100 object-cover"
							/>
							<div class="min-w-0 flex-1">
								<p class="truncate text-[13px] text-stone-800">
									{formState.coverImage.alt || 'Cover image'}
								</p>
								<button
									type="button"
									onclick={pickCoverImage}
									class="text-[12px] text-stone-500 underline-offset-2 hover:text-stone-800 hover:underline"
								>
									Change cover image
								</button>
							</div>
							<button
								type="button"
								aria-label="Remove cover image"
								onclick={clearCoverImage}
								class="flex h-6 w-6 shrink-0 items-center justify-center rounded-[3px] text-stone-400 hover:bg-stone-100 hover:text-stone-700"
							>
								<svg width="15" height="15" viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M14 6 6 14M6 6l8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
							</button>
						</div>
					{:else}
						<button
							type="button"
							onclick={pickCoverImage}
							class="flex w-full flex-col items-center gap-1.5 rounded-[4px] border border-dashed border-stone-200 px-3 py-5 text-[13px] text-stone-500 hover:border-stone-300 hover:bg-stone-50"
						>
							<svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.6" /><circle cx="9" cy="9" r="2" stroke="currentColor" stroke-width="1.6" /><path d="m21 15-5-5L5 21" stroke="currentColor" stroke-width="1.6" /></svg>
							<span>Pick cover image</span>
						</button>
					{/if}
					{#if fieldErrors.cover_image}
						<p class="text-xs text-destructive">{fieldErrors.cover_image}</p>
					{/if}
				</div>

				<div class="space-y-2">
					<span class="text-[13px] text-stone-500">OG image</span>
					{#if formState.ogImage}
						<div class="flex items-center gap-3 rounded-[4px] border border-stone-200 p-2">
							<img
								src={formState.ogImage.url}
								alt={formState.ogImage.alt}
								class="h-11 w-11 shrink-0 rounded-[3px] border border-stone-100 object-cover"
							/>
							<div class="min-w-0 flex-1">
								<p class="truncate text-[13px] text-stone-800">
									{formState.ogImage.alt || 'OG image'}
								</p>
								<button
									type="button"
									onclick={pickOgImage}
									class="text-[12px] text-stone-500 underline-offset-2 hover:text-stone-800 hover:underline"
								>
									Change OG image
								</button>
							</div>
							<button
								type="button"
								aria-label="Remove OG image"
								onclick={clearOgImage}
								class="flex h-6 w-6 shrink-0 items-center justify-center rounded-[3px] text-stone-400 hover:bg-stone-100 hover:text-stone-700"
							>
								<svg width="15" height="15" viewBox="0 0 20 20" fill="none" aria-hidden="true"><path d="M14 6 6 14M6 6l8 8" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" /></svg>
							</button>
						</div>
					{:else}
						<button
							type="button"
							onclick={pickOgImage}
							class="flex w-full flex-col items-center gap-1.5 rounded-[4px] border border-dashed border-stone-200 px-3 py-5 text-[13px] text-stone-500 hover:border-stone-300 hover:bg-stone-50"
						>
							<svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.6" /><circle cx="9" cy="9" r="2" stroke="currentColor" stroke-width="1.6" /><path d="m21 15-5-5L5 21" stroke="currentColor" stroke-width="1.6" /></svg>
							<span>Pick OG image</span>
						</button>
					{/if}
					{#if fieldErrors.og_image}
						<p class="text-xs text-destructive">{fieldErrors.og_image}</p>
					{/if}
				</div>

				<div>
					<label class="flex items-center gap-2 text-[13px] text-stone-700">
						<input type="checkbox" bind:checked={formState.featured} />
						Featured
					</label>
				</div>

				<div class="space-y-4 border-t border-stone-100 pt-6">
					<span class="text-[13px] text-stone-500">SEO</span>
					<div class="space-y-1.5">
						<Label for="seo-title">SEO title</Label>
						<Input
							id="seo-title"
							maxlength={70}
							bind:value={formState.seoTitle}
							aria-invalid={!!fieldErrors.seo_title}
						/>
						<p class="text-right text-xs text-stone-400">{formState.seoTitle.length}/70</p>
						{#if fieldErrors.seo_title}
							<p class="text-xs text-destructive">{fieldErrors.seo_title}</p>
						{/if}
					</div>
					<div class="space-y-1.5">
						<Label for="seo-description">SEO description</Label>
						<Textarea
							id="seo-description"
							maxlength={170}
							bind:value={formState.seoDescription}
							aria-invalid={!!fieldErrors.seo_description}
						/>
						<p class="text-right text-xs text-stone-400">
							{formState.seoDescription.length}/170
						</p>
						{#if fieldErrors.seo_description}
							<p class="text-xs text-destructive">{fieldErrors.seo_description}</p>
						{/if}
					</div>
					<div class="space-y-1.5">
						<Label for="canonical-url">Canonical URL</Label>
						<Input
							id="canonical-url"
							type="url"
							bind:value={formState.canonicalUrl}
							aria-invalid={!!fieldErrors.canonical_url}
						/>
						{#if fieldErrors.canonical_url}
							<p class="text-xs text-destructive">{fieldErrors.canonical_url}</p>
						{/if}
					</div>
				</div>
			</aside>
		</div>
	</div>

	<MediaPicker />
{/if}

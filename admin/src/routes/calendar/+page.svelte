<script lang="ts">
	/**
	 * Content scheduling calendar (v0.3): month grid of scheduled and
	 * published posts. Click a chip to open the editor; scheduled posts can
	 * be rescheduled inline without leaving the calendar.
	 */
	import { Badge } from '$lib/components/ui/badge/index.js';
	import { Button } from '$lib/components/ui/button/index.js';
	import { Input } from '$lib/components/ui/input/index.js';
	import { Label } from '$lib/components/ui/label/index.js';
	import {
		ApiError,
		listCalendar,
		schedulePost,
		type CalendarEntry
	} from '$lib/api';
	import {
		WEEKDAY_LABELS,
		buildMonthGrid,
		monthQueryRange,
		monthTitle,
		shiftMonth,
		utcDateKey,
		type CalendarDay
	} from '$lib/calendar';
	import { POST_STATUS_BADGE_CLASSES } from '$lib/post-status';
	import { cn } from '$lib/utils';

	type LoadState = 'pending' | 'success' | 'error';

	const now = new Date();
	let year = $state(now.getFullYear());
	let month = $state(now.getMonth() + 1);

	let entries = $state<CalendarEntry[]>([]);
	let loadState = $state<LoadState>('pending');
	let loadError = $state('');

	let selected = $state<CalendarEntry | null>(null);
	let rescheduleLocal = $state('');
	let rescheduleBusy = $state(false);
	let rescheduleError = $state('');
	let rescheduleFieldError = $state('');

	const days = $derived(buildMonthGrid(year, month));
	const title = $derived(monthTitle(year, month));

	const byDate = $derived.by(() => {
		const map = new Map<string, CalendarEntry[]>();
		for (const entry of entries) {
			const key = utcDateKey(entry.at);
			if (!key) continue;
			const list = map.get(key) ?? [];
			list.push(entry);
			map.set(key, list);
		}
		return map;
	});

	async function load() {
		loadState = 'pending';
		loadError = '';
		const { from, to } = monthQueryRange(year, month);
		try {
			const result = await listCalendar({ from, to });
			entries = result.entries;
			loadState = 'success';
		} catch (err) {
			loadError = err instanceof Error ? err.message : 'Unable to load calendar.';
			loadState = 'error';
		}
	}

	$effect(() => {
		// Re-fetch whenever the visible month changes.
		void year;
		void month;
		load();
	});

	function go(delta: number) {
		const next = shiftMonth(year, month, delta);
		year = next.year;
		month = next.month;
		selected = null;
	}

	function goToday() {
		const t = new Date();
		year = t.getFullYear();
		month = t.getMonth() + 1;
		selected = null;
	}

	function selectEntry(entry: CalendarEntry) {
		selected = entry;
		rescheduleError = '';
		rescheduleFieldError = '';
		if (entry.status === 'scheduled' && entry.scheduled_for) {
			rescheduleLocal = toDatetimeLocalValue(entry.scheduled_for);
		} else {
			rescheduleLocal = '';
		}
	}

	function toDatetimeLocalValue(iso: string): string {
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return '';
		const pad = (n: number) => String(n).padStart(2, '0');
		return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
	}

	function fromDatetimeLocalValue(local: string): string {
		const d = new Date(local);
		return d.toISOString();
	}

	function formatTime(iso: string): string {
		const d = new Date(iso);
		if (Number.isNaN(d.getTime())) return '';
		return d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
	}

	async function handleReschedule(event: SubmitEvent) {
		event.preventDefault();
		if (!selected || selected.status !== 'scheduled') return;
		rescheduleBusy = true;
		rescheduleError = '';
		rescheduleFieldError = '';
		try {
			const { post } = await schedulePost(selected.id, {
				scheduled_for: fromDatetimeLocalValue(rescheduleLocal)
			});
			const at = post.scheduled_for ?? selected.at;
			const updated: CalendarEntry = {
				id: post.id,
				title: post.title,
				slug: post.slug,
				status: post.status,
				locale: post.locale,
				author: { id: post.author.id, name: post.author.name, slug: post.author.slug },
				at,
				scheduled_for: post.scheduled_for,
				published_at: post.published_at
			};
			entries = entries.map((e) => (e.id === updated.id ? updated : e));
			selected = updated;
			rescheduleLocal = toDatetimeLocalValue(at);
		} catch (err) {
			if (err instanceof ApiError && err.code === 'validation_failed' && err.details?.scheduled_for) {
				rescheduleFieldError = err.details.scheduled_for;
			} else if (err instanceof ApiError) {
				rescheduleError = err.message;
			} else {
				rescheduleError = err instanceof Error ? err.message : 'Unable to reschedule.';
			}
		} finally {
			rescheduleBusy = false;
		}
	}

	function dayEntries(day: CalendarDay): CalendarEntry[] {
		return byDate.get(day.date) ?? [];
	}
</script>

<header class="mb-6 pt-2">
	<div class="flex flex-wrap items-center gap-3">
		<h1 class="text-[32px] font-medium leading-9 text-stone-900">Calendar</h1>
		<Button href="/posts/new" size="sm">New post</Button>
	</div>
	<p class="mt-2 text-[13px] text-stone-500">
		Upcoming schedules and published dates in one place.
	</p>
</header>

<div class="mb-4 flex flex-wrap items-center gap-2">
	<Button variant="outline" size="sm" onclick={() => go(-1)} aria-label="Previous month">
		Previous
	</Button>
	<h2 class="min-w-[10rem] text-center text-base font-medium text-stone-900">{title}</h2>
	<Button variant="outline" size="sm" onclick={() => go(1)} aria-label="Next month">Next</Button>
	<Button variant="ghost" size="sm" onclick={goToday}>Today</Button>
	<div class="ml-auto flex items-center gap-3 text-[12px] text-stone-500">
		<span class="inline-flex items-center gap-1.5">
			<span class="inline-block h-2.5 w-2.5 rounded-sm bg-amber-400"></span>
			Scheduled
		</span>
		<span class="inline-flex items-center gap-1.5">
			<span class="inline-block h-2.5 w-2.5 rounded-sm bg-teal-500"></span>
			Published
		</span>
	</div>
</div>

{#if loadState === 'error'}
	<p class="mb-4 text-sm text-red-600" role="alert">{loadError}</p>
{/if}

<div
	class="overflow-hidden rounded-md border border-stone-200 bg-white"
	aria-label="Scheduling calendar"
>
	<div class="grid grid-cols-7 border-b border-stone-200 bg-stone-50">
		{#each WEEKDAY_LABELS as label}
			<div class="px-2 py-2 text-center text-[11px] font-medium uppercase tracking-wide text-stone-500">
				{label}
			</div>
		{/each}
	</div>
	<div class="grid grid-cols-7">
		{#each days as day}
			<div
				class={cn(
					'min-h-[6.5rem] border-b border-r border-stone-100 p-1.5 last:border-r-0',
					!day.inMonth && 'bg-stone-50/60',
					day.isToday && 'bg-amber-50/40'
				)}
				data-date={day.date}
			>
				<span
					class={cn(
						'mb-1 inline-flex h-6 w-6 items-center justify-center rounded-full text-[12px]',
						day.inMonth ? 'text-stone-700' : 'text-stone-400',
						day.isToday && 'bg-stone-900 font-medium text-white'
					)}
				>
					{day.day}
				</span>
				<div class="flex flex-col gap-0.5">
					{#each dayEntries(day) as entry}
						<button
							type="button"
							class={cn(
								'truncate rounded px-1 py-0.5 text-left text-[11px] leading-4 transition-colors',
								entry.status === 'scheduled'
									? 'bg-amber-100 text-amber-900 hover:bg-amber-200'
									: 'bg-teal-100 text-teal-900 hover:bg-teal-200',
								selected?.id === entry.id && 'ring-1 ring-stone-400'
							)}
							title="{entry.title} · {formatTime(entry.at)}"
							onclick={() => selectEntry(entry)}
						>
							{entry.title || '(untitled)'}
						</button>
					{/each}
				</div>
			</div>
		{/each}
	</div>
</div>

{#if loadState === 'pending'}
	<p class="mt-3 text-sm text-stone-400">Loading…</p>
{:else if loadState === 'success' && entries.length === 0}
	<p class="mt-3 text-sm text-stone-500">No scheduled or published posts in this month.</p>
{/if}

{#if selected}
	<aside
		class="mt-6 rounded-md border border-stone-200 bg-white p-4"
		aria-label="Selected post"
	>
		<div class="flex flex-wrap items-start justify-between gap-3">
			<div>
				<div class="flex flex-wrap items-center gap-2">
					<h3 class="text-base font-medium text-stone-900">{selected.title || '(untitled)'}</h3>
					<Badge
						variant="outline"
						class={cn('capitalize', POST_STATUS_BADGE_CLASSES[selected.status])}
					>
						{selected.status}
					</Badge>
				</div>
				<p class="mt-1 text-[13px] text-stone-500">
					{formatTime(selected.at)}
					· {selected.author.name}
					{#if selected.locale}
						· {selected.locale}
					{/if}
				</p>
			</div>
			<div class="flex gap-2">
				<Button href="/posts/{selected.id}" size="sm">Open editor</Button>
				<Button
					variant="ghost"
					size="sm"
					onclick={() => {
						selected = null;
					}}
				>
					Close
				</Button>
			</div>
		</div>

		{#if selected.status === 'scheduled'}
			<form class="mt-4 flex flex-wrap items-end gap-3" onsubmit={handleReschedule}>
				<div class="space-y-1.5">
					<Label for="reschedule-at">Reschedule</Label>
					<Input
						id="reschedule-at"
						type="datetime-local"
						bind:value={rescheduleLocal}
						required
						class="w-auto"
					/>
					{#if rescheduleFieldError}
						<p class="text-[12px] text-red-600">{rescheduleFieldError}</p>
					{/if}
				</div>
				<Button type="submit" size="sm" disabled={rescheduleBusy}>
					{rescheduleBusy ? 'Saving…' : 'Update schedule'}
				</Button>
				{#if rescheduleError}
					<p class="w-full text-sm text-red-600" role="alert">{rescheduleError}</p>
				{/if}
			</form>
		{/if}
	</aside>
{/if}

/**
 * Pure helpers for the admin scheduling calendar month grid.
 * Week starts on Monday to match common editorial calendars in Europe.
 */

export interface CalendarDay {
	/** YYYY-MM-DD in local calendar terms (not a timezone conversion). */
	date: string;
	year: number;
	month: number; // 1–12
	day: number;
	/** True when this cell belongs to the displayed month. */
	inMonth: boolean;
	/** True when date equals today's local YYYY-MM-DD. */
	isToday: boolean;
}

/** Format a Y/M/D triple as zero-padded YYYY-MM-DD. */
export function formatDateKey(year: number, month: number, day: number): string {
	const m = String(month).padStart(2, '0');
	const d = String(day).padStart(2, '0');
	return `${year}-${m}-${d}`;
}

/** Local today as YYYY-MM-DD. */
export function todayKey(now: Date = new Date()): string {
	return formatDateKey(now.getFullYear(), now.getMonth() + 1, now.getDate());
}

/**
 * Build a Monday-start month grid (always 6 weeks / 42 cells) for the given
 * year and 1-based month.
 */
export function buildMonthGrid(year: number, month: number, now: Date = new Date()): CalendarDay[] {
	const first = new Date(year, month - 1, 1);
	// JS getDay(): 0=Sun … 6=Sat. Convert to Monday-start offset 0–6.
	const mondayOffset = (first.getDay() + 6) % 7;
	const start = new Date(year, month - 1, 1 - mondayOffset);
	const today = todayKey(now);

	const days: CalendarDay[] = [];
	for (let i = 0; i < 42; i++) {
		const cell = new Date(start.getFullYear(), start.getMonth(), start.getDate() + i);
		const y = cell.getFullYear();
		const m = cell.getMonth() + 1;
		const d = cell.getDate();
		const date = formatDateKey(y, m, d);
		days.push({
			date,
			year: y,
			month: m,
			day: d,
			inMonth: m === month && y === year,
			isToday: date === today
		});
	}
	return days;
}

/** Inclusive from/to YYYY-MM-DD covering every cell in the month grid. */
export function monthQueryRange(year: number, month: number): { from: string; to: string } {
	const days = buildMonthGrid(year, month);
	return { from: days[0].date, to: days[days.length - 1].date };
}

/** Shift a year/month by delta months (can be negative). */
export function shiftMonth(
	year: number,
	month: number,
	delta: number
): { year: number; month: number } {
	const idx = year * 12 + (month - 1) + delta;
	return { year: Math.floor(idx / 12), month: (idx % 12) + 1 };
}

/** YYYY-MM-DD of an RFC3339 / ISO timestamp (UTC date components). */
export function utcDateKey(iso: string): string {
	const d = new Date(iso);
	if (Number.isNaN(d.getTime())) return '';
	return formatDateKey(d.getUTCFullYear(), d.getUTCMonth() + 1, d.getUTCDate());
}

export const WEEKDAY_LABELS = ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'] as const;

export function monthTitle(year: number, month: number): string {
	const label = new Date(year, month - 1, 1).toLocaleString(undefined, {
		month: 'long',
		year: 'numeric'
	});
	return label;
}

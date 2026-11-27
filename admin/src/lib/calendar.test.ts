import { describe, expect, it } from 'vitest';

import {
	buildMonthGrid,
	formatDateKey,
	monthQueryRange,
	shiftMonth,
	todayKey,
	utcDateKey
} from './calendar';

describe('calendar helpers', () => {
	it('formats date keys with zero padding', () => {
		expect(formatDateKey(2026, 3, 5)).toBe('2026-03-05');
		expect(formatDateKey(2026, 11, 27)).toBe('2026-11-27');
	});

	it('builds a 42-cell Monday-start November 2026 grid', () => {
		// 2026-11-01 is a Sunday → Monday-start grid begins on 2026-10-26.
		const fixed = new Date(2026, 10, 15); // local Nov 15
		const days = buildMonthGrid(2026, 11, fixed);
		expect(days).toHaveLength(42);
		expect(days[0].date).toBe('2026-10-26');
		expect(days[0].inMonth).toBe(false);
		expect(days[6].date).toBe('2026-11-01');
		expect(days[6].inMonth).toBe(true);
		expect(days[days.length - 1].date).toBe('2026-12-06');
	});

	it('marks today inside the grid', () => {
		const now = new Date(2026, 10, 27);
		const days = buildMonthGrid(2026, 11, now);
		const today = days.find((d) => d.date === '2026-11-27');
		expect(today?.isToday).toBe(true);
		expect(todayKey(now)).toBe('2026-11-27');
	});

	it('returns the inclusive query range for the visible grid', () => {
		expect(monthQueryRange(2026, 11)).toEqual({
			from: '2026-10-26',
			to: '2026-12-06'
		});
	});

	it('shifts months across year boundaries', () => {
		expect(shiftMonth(2026, 1, -1)).toEqual({ year: 2025, month: 12 });
		expect(shiftMonth(2026, 12, 1)).toEqual({ year: 2027, month: 1 });
		expect(shiftMonth(2026, 11, 0)).toEqual({ year: 2026, month: 11 });
	});

	it('extracts a UTC date key from an ISO timestamp', () => {
		expect(utcDateKey('2026-11-15T10:00:00Z')).toBe('2026-11-15');
		expect(utcDateKey('not-a-date')).toBe('');
	});
});

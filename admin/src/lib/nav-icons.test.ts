import { describe, expect, it } from 'vitest';
import { NAV_GROUPS, type NavKey } from '$lib/nav';
import { NAV_ICONS, NAV_ICON_SIZE, NAV_ICON_STROKE_WIDTH } from '$lib/nav-icons';

describe('NAV_ICONS', () => {
	it('maps every sidebar nav key to a Hugeicons glyph', () => {
		const keys = NAV_GROUPS.flatMap((g) => g.items.map((i) => i.key));
		expect(keys.length).toBeGreaterThan(0);
		for (const key of keys) {
			expect(NAV_ICONS[key as NavKey]).toBeTruthy();
			expect(Array.isArray(NAV_ICONS[key as NavKey])).toBe(true);
		}
	});

	it('uses Animate UI Hugeicons stroke width and a compact sidebar size', () => {
		expect(NAV_ICON_STROKE_WIDTH).toBe(2);
		expect(NAV_ICON_SIZE).toBe(16);
	});
});

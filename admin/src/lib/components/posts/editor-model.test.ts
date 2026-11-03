import { describe, expect, it } from 'vitest';
import {
	floatingToolbarPosition,
	addButtonTop,
	filterBlockMenu,
	flatBlockItems,
	isEditorEmpty,
	BLOCK_MENU,
	type Rect
} from './editor-model';

describe('isEditorEmpty (placeholder predicate)', () => {
	it('is empty only for no text in a single (or zero) top-level block', () => {
		expect(isEditorEmpty('', 0)).toBe(true);
		expect(isEditorEmpty('', 1)).toBe(true); // Lexical's empty = one empty paragraph
		expect(isEditorEmpty('hi', 1)).toBe(false); // typed one char -> hidden
		expect(isEditorEmpty('', 2)).toBe(false); // e.g. an empty line after an HR/image
		expect(isEditorEmpty('   ', 1)).toBe(false); // whitespace counts as content
	});
});

function rect(left: number, top: number, width: number, height: number): Rect {
	return { left, top, width, height, right: left + width, bottom: top + height };
}

describe('floatingToolbarPosition', () => {
	const container = rect(100, 100, 800, 600); // viewport-space container
	const toolbar = { width: 320, height: 40 };

	it('centres horizontally over the selection, relative to the container', () => {
		// selection centred at viewport x=500 -> container-relative center 400
		const sel = rect(460, 300, 80, 20);
		const pos = floatingToolbarPosition(sel, container, toolbar);
		// left = 400 - 160 = 240
		expect(pos.left).toBe(240);
	});

	it('places the toolbar above the selection when there is room (caret points down)', () => {
		const sel = rect(400, 300, 80, 20); // sel top viewport 300 -> rel 200
		const pos = floatingToolbarPosition(sel, container, toolbar, 8);
		expect(pos.below).toBe(false);
		expect(pos.top).toBe(200 - 40 - 8); // 152
	});

	it('flips below when the selection is too close to the container top', () => {
		const sel = rect(400, 110, 80, 20); // sel top rel 10 -> above would be 10-40-8<edge
		const pos = floatingToolbarPosition(sel, container, toolbar, 8);
		expect(pos.below).toBe(true);
		// top = sel.bottom(130) - container.top(100) + gap(8) = 38
		expect(pos.top).toBe(38);
	});

	it('clamps left within the container edges', () => {
		const near = floatingToolbarPosition(rect(100, 300, 10, 20), container, toolbar, 8, 6);
		expect(near.left).toBe(6); // clamped to left edge
		const far = floatingToolbarPosition(rect(880, 300, 10, 20), container, toolbar, 8, 6);
		expect(far.left).toBe(800 - 320 - 6); // clamped to right edge = 474
	});
});

describe('addButtonTop', () => {
	it('returns the block top relative to the container, never negative', () => {
		const container = rect(0, 100, 500, 400);
		expect(addButtonTop(rect(0, 160, 100, 24), container)).toBe(60);
		expect(addButtonTop(rect(0, 40, 100, 24), container)).toBe(0);
	});
});

describe('block menu catalogue', () => {
	it('has unique actions and every item is labelled', () => {
		const items = flatBlockItems();
		const actions = items.map((i) => i.action);
		expect(new Set(actions).size).toBe(actions.length);
		for (const it of items) expect(it.label.length).toBeGreaterThan(0);
	});

	it('includes the core Payload blocks', () => {
		const actions = flatBlockItems().map((i) => i.action);
		for (const a of ['h1', 'h2', 'h3', 'bullet', 'number', 'quote', 'code', 'hr', 'image']) {
			expect(actions).toContain(a);
		}
	});

	it('groups blocks like Payload (Basic / Lists / Headings)', () => {
		expect(BLOCK_MENU.map((g) => g.title)).toEqual(['Basic', 'Lists', 'Headings']);
	});
});

describe('filterBlockMenu', () => {
	it('returns the full grouped menu for an empty query', () => {
		expect(filterBlockMenu('')).toBe(BLOCK_MENU);
		expect(filterBlockMenu('   ')).toBe(BLOCK_MENU);
	});

	it('matches labels and keywords case-insensitively', () => {
		const heads = filterBlockMenu('head').flatMap((g) => g.items).map((i) => i.action);
		expect(heads).toEqual(['h1', 'h2', 'h3']);

		const img = filterBlockMenu('photo').flatMap((g) => g.items).map((i) => i.action);
		expect(img).toEqual(['image']);

		const list = filterBlockMenu('LIST').flatMap((g) => g.items).map((i) => i.action);
		expect(list).toEqual(['bullet', 'number']);
	});

	it('drops empty groups and returns nothing for no match', () => {
		expect(filterBlockMenu('zzznomatch')).toEqual([]);
	});
});

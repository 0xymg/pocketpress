/**
 * Framework-free model + geometry for the Lexical editor's Payload-style
 * floating UI (LexicalEditor.svelte). Kept pure so the positioning math and the
 * block-menu catalogue can be unit-tested without a DOM or a live editor.
 */

export interface Rect {
	left: number;
	top: number;
	right: number;
	bottom: number;
	width: number;
	height: number;
}

export interface Size {
	width: number;
	height: number;
}

export interface FloatingPos {
	/** Left offset, in px, relative to the positioned editor container. */
	left: number;
	/** Top offset, in px, relative to the positioned editor container. */
	top: number;
	/** When true the toolbar sits below the selection (caret points up). */
	below: boolean;
}

function clamp(v: number, lo: number, hi: number): number {
	if (hi < lo) return lo;
	return Math.min(Math.max(v, lo), hi);
}

/**
 * Positions the floating selection toolbar over a selection rect, mirroring
 * Payload's inline-toolbar-popup: horizontally centred on the selection and
 * clamped inside the container, preferring just above the selection and
 * flipping below when there isn't room. All inputs/outputs are in viewport
 * pixels except the returned left/top which are relative to `container`.
 */
export function floatingToolbarPosition(
	selection: Rect,
	container: Rect,
	toolbar: Size,
	gap = 8,
	edge = 6
): FloatingPos {
	const selCenterX = selection.left + selection.width / 2;
	let left = selCenterX - container.left - toolbar.width / 2;
	left = clamp(left, edge, container.width - toolbar.width - edge);

	const aboveTop = selection.top - container.top - toolbar.height - gap;
	if (aboveTop >= edge) {
		return { left, top: aboveTop, below: false };
	}
	return { left, top: selection.bottom - container.top + gap, below: true };
}

/**
 * Vertical offset (relative to the container) for the left-gutter "+" add-block
 * button, aligned to the top of the active block's DOM rect.
 */
export function addButtonTop(block: Rect, container: Rect): number {
	return Math.max(0, block.top - container.top);
}

/**
 * Whether the editor is "empty" for placeholder purposes: no text and at most
 * one top-level block (Lexical's empty state is a single empty paragraph). The
 * placeholder overlay is shown exactly when this is true — independent of
 * focus, matching Payload.
 */
export function isEditorEmpty(textContent: string, blockCount: number): boolean {
	return textContent === '' && blockCount <= 1;
}

export type BlockAction =
	| 'paragraph'
	| 'h1'
	| 'h2'
	| 'h3'
	| 'quote'
	| 'code'
	| 'bullet'
	| 'number'
	| 'hr'
	| 'image';

export interface BlockMenuItem {
	action: BlockAction;
	label: string;
	/** Keyword hints for slash-menu filtering, beyond the label. */
	keywords?: string[];
}

export interface BlockMenuGroup {
	title: string;
	items: BlockMenuItem[];
}

/**
 * The block catalogue shown by both the "+" inserter and the "/" slash menu,
 * grouped like Payload's block menu.
 */
export const BLOCK_MENU: BlockMenuGroup[] = [
	{
		title: 'Basic',
		items: [
			{ action: 'paragraph', label: 'Paragraph', keywords: ['text', 'p'] },
			{ action: 'quote', label: 'Blockquote', keywords: ['quote'] },
			{ action: 'code', label: 'Code block', keywords: ['code', 'pre'] },
			{ action: 'hr', label: 'Horizontal Rule', keywords: ['divider', 'hr', 'line'] },
			{ action: 'image', label: 'Image', keywords: ['img', 'photo', 'upload', 'media'] }
		]
	},
	{
		title: 'Lists',
		items: [
			{ action: 'bullet', label: 'Unordered List', keywords: ['bullet', 'ul'] },
			{ action: 'number', label: 'Ordered List', keywords: ['numbered', 'ol'] }
		]
	},
	{
		title: 'Headings',
		items: [
			{ action: 'h1', label: 'Heading 1', keywords: ['h1', 'title'] },
			{ action: 'h2', label: 'Heading 2', keywords: ['h2'] },
			{ action: 'h3', label: 'Heading 3', keywords: ['h3'] }
		]
	}
];

/** Flat list of every block item, in menu order. */
export function flatBlockItems(): BlockMenuItem[] {
	return BLOCK_MENU.flatMap((g) => g.items);
}

/**
 * Filters the block catalogue by a slash-menu query (case-insensitive, matched
 * against label + keywords). An empty query returns the full grouped menu.
 */
export function filterBlockMenu(query: string): BlockMenuGroup[] {
	const q = query.trim().toLowerCase();
	if (!q) return BLOCK_MENU;
	const groups: BlockMenuGroup[] = [];
	for (const group of BLOCK_MENU) {
		const items = group.items.filter((item) => {
			const hay = [item.label, ...(item.keywords ?? [])].join(' ').toLowerCase();
			return hay.includes(q);
		});
		if (items.length) groups.push({ title: group.title, items });
	}
	return groups;
}

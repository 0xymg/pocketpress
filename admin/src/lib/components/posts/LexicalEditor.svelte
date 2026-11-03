<script lang="ts" module>
	/** Image supplied to the editor — by URL prompt today, by the media browser later. */
	export interface InsertedImage {
		src: string;
		alt?: string;
	}

	/**
	 * Hook the "insert image" action calls. Defaults to a URL prompt; the media
	 * picker passes its own implementation. Returning null cancels the insert.
	 */
	export type InsertImageHook = () => Promise<InsertedImage | null>;

	export interface LexicalEditorProps {
		/** Serialized Lexical editor state JSON to load. '', '{}' and invalid → empty. */
		initialJson?: string;
		/** Fired once, right after mount, with the editor's canonical serialization. */
		onReady?: (json: string, html: string) => void;
		/** Debounced (~300ms) on every content change: (contentJson, contentHtml). */
		onChange?: (json: string, html: string) => void;
		/** Media hook — see InsertImageHook. Omitted → URL prompt. */
		onInsertImage?: InsertImageHook;
		/** Marks the field invalid (mirrors aria-invalid on the shadcn inputs). */
		invalid?: boolean;
		/** Accessible name for the content region. */
		ariaLabel?: string;
	}
</script>

<script lang="ts">
	/**
	 * Rich-text editor matching Payload's Lexical UX: no fixed toolbar. Instead a
	 * floating inline toolbar appears over a text selection (format + link), a
	 * "+" button in the left gutter opens a block menu on the active line, and
	 * typing "/" opens the same block menu as a slash command. All popover
	 * buttons preventDefault on mousedown so the editor keeps focus and the live
	 * selection — the earlier fixed toolbar both stole that focus and, being
	 * sticky, overlapped and hid the first line of text.
	 */
	import { onMount, onDestroy } from 'svelte';
	import {
		createEditor,
		$getRoot as getRoot,
		$getSelection as getSelection,
		$isRangeSelection as isRangeSelection,
		$createParagraphNode as createParagraphNode,
		$insertNodes as insertNodes,
		$getNodeByKey as getNodeByKey,
		FORMAT_TEXT_COMMAND,
		PASTE_COMMAND,
		KEY_ARROW_DOWN_COMMAND,
		KEY_ARROW_UP_COMMAND,
		KEY_ENTER_COMMAND,
		KEY_ESCAPE_COMMAND,
		COMMAND_PRIORITY_LOW,
		COMMAND_PRIORITY_HIGH,
		type LexicalEditor,
		type LexicalNode,
		type TextFormatType
	} from 'lexical';
	import {
		registerRichText,
		HeadingNode,
		QuoteNode,
		$createHeadingNode as createHeadingNode,
		$createQuoteNode as createQuoteNode,
		$isHeadingNode as isHeadingNode,
		$isQuoteNode as isQuoteNode,
		type HeadingTagType
	} from '@lexical/rich-text';
	import {
		registerList,
		ListNode,
		ListItemNode,
		INSERT_UNORDERED_LIST_COMMAND,
		INSERT_ORDERED_LIST_COMMAND,
		REMOVE_LIST_COMMAND,
		$isListNode as isListNode
	} from '@lexical/list';
	import {
		LinkNode,
		TOGGLE_LINK_COMMAND,
		$toggleLink as toggleLinkNodes,
		$isLinkNode as isLinkNode
	} from '@lexical/link';
	import {
		CodeNode,
		CodeHighlightNode,
		$createCodeNode as createCodeNode,
		$isCodeNode as isCodeNode
	} from '@lexical/code';
	import { $generateHtmlFromNodes as generateHtmlFromNodes } from '@lexical/html';
	import { registerHistory, createEmptyHistoryState } from '@lexical/history';
	import { $setBlocksType as setBlocksType } from '@lexical/selection';
	import {
		mergeRegister,
		$getNearestNodeOfType as getNearestNodeOfType,
		$insertNodeToNearestRoot as insertNodeToNearestRoot
	} from '@lexical/utils';
	import {
		registerMarkdownShortcuts,
		HEADING,
		QUOTE,
		UNORDERED_LIST,
		ORDERED_LIST,
		CODE,
		LINK,
		TEXT_FORMAT_TRANSFORMERS
	} from '@lexical/markdown';
	import { cn } from '$lib/utils.js';
	import { ImageNode, $createImageNode as createImageNode } from './lexical/image-node';
	import {
		HorizontalRuleNode,
		$createHorizontalRuleNode as createHorizontalRuleNode
	} from './lexical/hr-node';
	import {
		floatingToolbarPosition,
		addButtonTop,
		filterBlockMenu,
		isEditorEmpty,
		type BlockAction,
		type BlockMenuGroup
	} from './editor-model';

	// Markdown shortcuts matching our block/format set (no checklist/image
	// markdown — those blocks are inserted via the "+"/slash menu instead).
	const MARKDOWN_TRANSFORMERS = [
		HEADING,
		QUOTE,
		UNORDERED_LIST,
		ORDERED_LIST,
		CODE,
		LINK,
		...TEXT_FORMAT_TRANSFORMERS
	];

	let {
		initialJson = '',
		onReady,
		onChange,
		onInsertImage,
		invalid = false,
		ariaLabel = 'Content'
	}: LexicalEditorProps = $props();

	let rootEl: HTMLDivElement;
	let containerEl: HTMLDivElement;
	let floatingEl = $state<HTMLDivElement | null>(null);
	let editor: LexicalEditor;
	let unregister: () => void = () => {};
	let changeTimer: ReturnType<typeof setTimeout> | undefined;
	let uiRaf = 0;

	// --- Inline format + block state (read from the live selection) ----------
	type BlockType = 'paragraph' | 'h1' | 'h2' | 'h3' | 'quote' | 'code' | 'bullet' | 'number';
	let blockType = $state<BlockType>('paragraph');
	let isBold = $state(false);
	let isItalic = $state(false);
	let isUnderline = $state(false);
	let isStrikethrough = $state(false);
	let isCode = $state(false);
	let isSubscript = $state(false);
	let isSuperscript = $state(false);
	let isLink = $state(false);
	let contentEmpty = $state(true);
	let editorFocused = $state(false);

	// Non-reactive selection facts captured in the read, consumed by syncUi().
	let hasTextSelection = false;
	let activeBlockKey: string | null = null;
	let slashParaKey: string | null = null;
	let slashQuery = '';
	let linkNodeKey: string | null = null;
	let linkNodeUrl = '';

	let linkPopover = $state<{ visible: boolean; url: string; left: number; top: number }>({
		visible: false,
		url: '',
		left: 0,
		top: 0
	});

	function isHttpUrl(s: string): boolean {
		return /^https?:\/\/\S+$/i.test(s);
	}

	// --- Floating toolbar / "+" / block menu (reactive UI) -------------------
	let floating = $state<{ visible: boolean; left: number; top: number; below: boolean }>({
		visible: false,
		left: 0,
		top: 0,
		below: false
	});
	let addBtn = $state<{ visible: boolean; top: number }>({ visible: false, top: 0 });
	let blockMenu = $state<{
		open: boolean;
		mode: 'add' | 'slash';
		left: number;
		top: number;
		groups: BlockMenuGroup[];
		active: number;
	}>({ open: false, mode: 'add', left: 0, top: 0, groups: [], active: 0 });

	const flatMenu = $derived(blockMenu.groups.flatMap((g) => g.items));

	const theme = {
		paragraph: 'pp-p',
		heading: { h1: 'pp-h1', h2: 'pp-h2', h3: 'pp-h3', h4: 'pp-h4', h5: 'pp-h5', h6: 'pp-h6' },
		quote: 'pp-quote',
		list: { ul: 'pp-ul', ol: 'pp-ol', listitem: 'pp-li' },
		link: 'pp-link',
		code: 'pp-code-block',
		text: {
			bold: 'pp-bold',
			italic: 'pp-italic',
			underline: 'pp-underline',
			strikethrough: 'pp-strikethrough',
			code: 'pp-inline-code',
			subscript: 'pp-sub',
			superscript: 'pp-sup'
		}
	};

	function serialize(): [string, string] {
		const state = editor.getEditorState();
		const json = JSON.stringify(state.toJSON());
		let html = '';
		state.read(() => {
			html = generateHtmlFromNodes(editor, null);
		});
		return [json, html];
	}

	function scheduleChange() {
		clearTimeout(changeTimer);
		changeTimer = setTimeout(() => {
			const [json, html] = serialize();
			onChange?.(json, html);
		}, 300);
	}

	function refreshToolbar() {
		const selection = getSelection();
		const root = getRoot();
		contentEmpty = isEditorEmpty(root.getTextContent(), root.getChildrenSize());

		hasTextSelection = false;
		activeBlockKey = null;
		slashParaKey = null;
		slashQuery = '';

		if (!isRangeSelection(selection)) return;

		isBold = selection.hasFormat('bold');
		isItalic = selection.hasFormat('italic');
		isUnderline = selection.hasFormat('underline');
		isStrikethrough = selection.hasFormat('strikethrough');
		isCode = selection.hasFormat('code');
		isSubscript = selection.hasFormat('subscript');
		isSuperscript = selection.hasFormat('superscript');

		const anchorNode = selection.anchor.getNode();
		const topLevel =
			anchorNode.getKey() === 'root' ? anchorNode : anchorNode.getTopLevelElementOrThrow();
		activeBlockKey = topLevel.getKey();

		if (isListNode(topLevel)) {
			const nearestList = getNearestNodeOfType(anchorNode, ListNode) ?? topLevel;
			blockType = nearestList.getListType() === 'number' ? 'number' : 'bullet';
		} else if (isHeadingNode(topLevel)) {
			const tag = topLevel.getTag();
			blockType = tag === 'h1' || tag === 'h2' || tag === 'h3' ? tag : 'paragraph';
		} else if (isCodeNode(topLevel)) {
			blockType = 'code';
		} else if (isQuoteNode(topLevel)) {
			blockType = 'quote';
		} else {
			blockType = 'paragraph';
		}

		const parent = anchorNode.getParent();
		const linkNode: LexicalNode | null = isLinkNode(parent)
			? parent
			: isLinkNode(anchorNode)
				? anchorNode
				: null;
		isLink = !!linkNode;
		// Link popover shows for a collapsed caret resting inside a link.
		if (linkNode && selection.isCollapsed() && isLinkNode(linkNode)) {
			linkNodeKey = linkNode.getKey();
			linkNodeUrl = linkNode.getURL();
		} else {
			linkNodeKey = null;
			linkNodeUrl = '';
		}

		hasTextSelection = !selection.isCollapsed() && selection.getTextContent().trim().length > 0;

		// Slash-menu trigger: a collapsed caret in a top-level paragraph or
		// heading whose text starts with "/" (no space yet). Query is whatever
		// follows. A space cancels it, matching Payload.
		const topType = topLevel.getType();
		if (selection.isCollapsed() && (topType === 'paragraph' || topType === 'heading')) {
			const text = topLevel.getTextContent();
			if (text.startsWith('/') && !text.includes(' ')) {
				slashParaKey = topLevel.getKey();
				slashQuery = text.slice(1);
			}
		}
	}

	function scheduleUiSync() {
		cancelAnimationFrame(uiRaf);
		uiRaf = requestAnimationFrame(syncUi);
	}

	function syncUi() {
		if (!containerEl || !editor) return;
		const container = containerEl.getBoundingClientRect();

		// Floating selection toolbar.
		let floatingVisible = false;
		if (hasTextSelection) {
			const domSel = window.getSelection();
			if (domSel && domSel.rangeCount > 0 && !domSel.isCollapsed) {
				const rect = domSel.getRangeAt(0).getBoundingClientRect();
				if (rect.width > 0 || rect.height > 0) {
					const size = floatingEl
						? { width: floatingEl.offsetWidth || 320, height: floatingEl.offsetHeight || 40 }
						: { width: 320, height: 40 };
					const pos = floatingToolbarPosition(rect, container, size);
					floating = { visible: true, ...pos };
					floatingVisible = true;
				}
			}
		}
		if (!floatingVisible && floating.visible) floating = { ...floating, visible: false };

		// Link popover (collapsed caret inside a link; mutually exclusive with
		// the selection toolbar).
		if (linkNodeKey && !floatingVisible) {
			const el = editor.getElementByKey(linkNodeKey);
			if (el) {
				const r = el.getBoundingClientRect();
				linkPopover = {
					visible: true,
					url: linkNodeUrl,
					left: Math.max(6, r.left - container.left),
					top: r.bottom - container.top + 6
				};
			} else {
				linkPopover = { ...linkPopover, visible: false };
			}
		} else if (linkPopover.visible) {
			linkPopover = { ...linkPopover, visible: false };
		}

		// Slash menu.
		if (slashParaKey) {
			const el = editor.getElementByKey(slashParaKey);
			if (el) {
				const br = el.getBoundingClientRect();
				openBlockMenu('slash', br.left - container.left + 8, br.bottom - container.top + 4, slashQuery);
			}
		} else if (blockMenu.open && blockMenu.mode === 'slash') {
			closeBlockMenu();
		}

		// "+" add-block gutter button (aligned to the active block).
		if (editorFocused && activeBlockKey && !floatingVisible) {
			const el = editor.getElementByKey(activeBlockKey);
			addBtn = el
				? { visible: true, top: addButtonTop(el.getBoundingClientRect(), container) }
				: { visible: false, top: 0 };
		} else if (!(blockMenu.open && blockMenu.mode === 'add')) {
			addBtn = { visible: false, top: 0 };
		}
	}

	function loadInitial() {
		const json = (initialJson ?? '').trim();
		let applied = false;
		if (json && json !== '{}') {
			try {
				const state = editor.parseEditorState(json);
				if (!state.isEmpty()) {
					editor.setEditorState(state);
					applied = true;
				}
			} catch {
				applied = false;
			}
		}
		if (!applied) {
			editor.update(
				() => {
					const root = getRoot();
					if (root.getChildrenSize() === 0) root.append(createParagraphNode());
				},
				{ discrete: true }
			);
		}
		const [j, h] = serialize();
		onReady?.(j, h);
	}

	onMount(() => {
		editor = createEditor({
			namespace: 'pocketpress',
			theme,
			nodes: [
				HeadingNode,
				QuoteNode,
				ListNode,
				ListItemNode,
				LinkNode,
				CodeNode,
				CodeHighlightNode,
				ImageNode,
				HorizontalRuleNode
			],
			onError: (error) => console.error('[LexicalEditor]', error)
		});
		editor.setRootElement(rootEl);

		unregister = mergeRegister(
			registerRichText(editor),
			registerHistory(editor, createEmptyHistoryState(), 1000),
			registerList(editor),
			registerMarkdownShortcuts(editor, MARKDOWN_TRANSFORMERS),
			editor.registerCommand(
				TOGGLE_LINK_COMMAND,
				(payload) => {
					if (payload === null) {
						toggleLinkNodes(null);
					} else if (typeof payload === 'string') {
						toggleLinkNodes(payload);
					} else {
						const { url, ...rest } = payload;
						toggleLinkNodes(url, rest);
					}
					return true;
				},
				COMMAND_PRIORITY_LOW
			),
			// Paste a raw URL over selected text → wrap it in a link (Payload's
			// auto-link-on-paste). Any other paste falls through to the default.
			editor.registerCommand(
				PASTE_COMMAND,
				(event) => {
					const selection = getSelection();
					if (!isRangeSelection(selection) || selection.isCollapsed()) return false;
					const clip = (event as ClipboardEvent).clipboardData?.getData('text/plain') ?? '';
					const url = clip.trim();
					if (!isHttpUrl(url)) return false;
					event.preventDefault();
					editor.dispatchCommand(TOGGLE_LINK_COMMAND, url);
					return true;
				},
				COMMAND_PRIORITY_HIGH
			),
			// Block-menu keyboard nav intercepts Enter/Arrows/Escape as high-priority
			// Lexical commands (not a DOM handler) so the default rich-text Enter
			// can't also insert a newline while the menu is open.
			editor.registerCommand(
				KEY_ARROW_DOWN_COMMAND,
				(event) => {
					if (!blockMenu.open) return false;
					event?.preventDefault();
					moveMenu(1);
					return true;
				},
				COMMAND_PRIORITY_HIGH
			),
			editor.registerCommand(
				KEY_ARROW_UP_COMMAND,
				(event) => {
					if (!blockMenu.open) return false;
					event?.preventDefault();
					moveMenu(-1);
					return true;
				},
				COMMAND_PRIORITY_HIGH
			),
			editor.registerCommand(
				KEY_ENTER_COMMAND,
				(event) => {
					if (!blockMenu.open) return false;
					const items = blockMenu.groups.flatMap((g) => g.items);
					if (!items.length) return false;
					event?.preventDefault();
					chooseBlock(items[blockMenu.active].action);
					return true;
				},
				COMMAND_PRIORITY_HIGH
			),
			editor.registerCommand(
				KEY_ESCAPE_COMMAND,
				() => {
					if (!blockMenu.open) return false;
					closeBlockMenu();
					return true;
				},
				COMMAND_PRIORITY_HIGH
			),
			editor.registerUpdateListener(({ editorState, dirtyElements, dirtyLeaves }) => {
				editorState.read(() => refreshToolbar());
				scheduleUiSync();
				if (dirtyElements.size === 0 && dirtyLeaves.size === 0) return;
				scheduleChange();
			}),
			editor.registerRootListener((next, prev) => {
				if (prev) {
					prev.removeEventListener('focus', onEditorFocus);
					prev.removeEventListener('blur', onEditorBlur);
				}
				if (next) {
					next.addEventListener('focus', onEditorFocus);
					next.addEventListener('blur', onEditorBlur);
				}
			})
		);

		loadInitial();
		window.addEventListener('scroll', scheduleUiSync, true);
		window.addEventListener('resize', scheduleUiSync);
	});

	onDestroy(() => {
		clearTimeout(changeTimer);
		cancelAnimationFrame(uiRaf);
		unregister();
		editor?.setRootElement(null);
		window.removeEventListener('scroll', scheduleUiSync, true);
		window.removeEventListener('resize', scheduleUiSync);
	});

	function onEditorFocus() {
		editorFocused = true;
		scheduleUiSync();
	}
	function onEditorBlur() {
		// Delay so a click on the floating toolbar / block menu isn't treated as
		// leaving the editor.
		setTimeout(() => {
			const active = document.activeElement;
			if (containerEl && active && containerEl.contains(active)) return;
			editorFocused = false;
			floating = { ...floating, visible: false };
			addBtn = { visible: false, top: 0 };
			blockHandle = { ...blockHandle, open: false };
		}, 120);
	}

	// --- Actions -------------------------------------------------------------
	function format(type: TextFormatType) {
		editor.dispatchCommand(FORMAT_TEXT_COMMAND, type);
	}

	function applyBlock(action: BlockAction) {
		if (action === 'image') {
			insertImage();
			return;
		}
		if (action === 'hr') {
			editor.update(() => insertNodeToNearestRoot(createHorizontalRuleNode()));
			return;
		}
		if (action === 'bullet') {
			editor.dispatchCommand(INSERT_UNORDERED_LIST_COMMAND, undefined);
			return;
		}
		if (action === 'number') {
			editor.dispatchCommand(INSERT_ORDERED_LIST_COMMAND, undefined);
			return;
		}
		if (blockType === 'bullet' || blockType === 'number') {
			editor.dispatchCommand(REMOVE_LIST_COMMAND, undefined);
		}
		editor.update(() => {
			const selection = getSelection();
			if (!isRangeSelection(selection)) return;
			if (action === 'h1' || action === 'h2' || action === 'h3') {
				setBlocksType(selection, () => createHeadingNode(action as HeadingTagType));
			} else if (action === 'quote') {
				setBlocksType(selection, () => createQuoteNode());
			} else if (action === 'code') {
				setBlocksType(selection, () => createCodeNode());
			} else {
				setBlocksType(selection, () => createParagraphNode());
			}
		});
	}

	function onBlockSelect(event: Event) {
		applyBlock((event.currentTarget as HTMLSelectElement).value as BlockAction);
	}

	// --- Block drag/move handle -------------------------------------------------
	// A faithful pointer-drag reorder in vanilla Lexical is fragile (drop-target
	// hit-testing across decorator/list nodes), so the six-dot handle opens a
	// Move up / Move down / Delete menu instead — solid and keyboard-reachable.
	let blockHandle = $state<{ open: boolean; top: number }>({ open: false, top: 0 });

	function toggleHandleMenu() {
		blockHandle = { open: !blockHandle.open, top: addBtn.top };
	}

	function moveBlock(dir: 'up' | 'down') {
		const key = activeBlockKey;
		blockHandle = { ...blockHandle, open: false };
		if (!key) return;
		editor.update(() => {
			const node = getNodeByKey(key);
			if (!node) return;
			if (dir === 'up') {
				const prev = node.getPreviousSibling();
				if (prev) prev.insertBefore(node);
			} else {
				const next = node.getNextSibling();
				if (next) next.insertAfter(node);
			}
		});
		editor.focus();
	}

	function deleteBlock() {
		const key = activeBlockKey;
		blockHandle = { ...blockHandle, open: false };
		if (!key) return;
		editor.update(() => getNodeByKey(key)?.remove());
		editor.focus();
	}

	function editLink() {
		const url = window.prompt('Link URL (leave blank to remove)', isLink ? '' : 'https://');
		if (url === null) return;
		editor.dispatchCommand(TOGGLE_LINK_COMMAND, url.trim() || null);
	}

	async function defaultInsertImage(): Promise<InsertedImage | null> {
		const src = window.prompt('Image URL');
		if (!src || !src.trim()) return null;
		const alt = window.prompt('Alt text (optional)') ?? '';
		return { src: src.trim(), alt };
	}

	async function insertImage() {
		const provider = onInsertImage ?? defaultInsertImage;
		const result = await provider();
		if (!result || !result.src) return;
		editor.update(() => {
			insertNodes([createImageNode({ src: result.src, alt: result.alt ?? '' })]);
		});
	}

	// --- Block menu (shared by "+" and "/") ----------------------------------
	function openBlockMenu(mode: 'add' | 'slash', left: number, top: number, query = '') {
		const groups = filterBlockMenu(query);
		const count = groups.flatMap((g) => g.items).length;
		const keepActive = blockMenu.open && blockMenu.mode === mode;
		blockMenu = {
			open: true,
			mode,
			left,
			top,
			groups,
			active: keepActive ? Math.min(blockMenu.active, Math.max(0, count - 1)) : 0
		};
	}

	function closeBlockMenu() {
		if (blockMenu.open) blockMenu = { ...blockMenu, open: false };
	}

	function toggleAddMenu() {
		if (blockMenu.open && blockMenu.mode === 'add') {
			closeBlockMenu();
			return;
		}
		openBlockMenu('add', 32, addBtn.top + 30, '');
	}

	function chooseBlock(action: BlockAction) {
		const mode = blockMenu.mode;
		const paraKey = slashParaKey;
		closeBlockMenu();
		if (mode === 'slash' && paraKey) {
			// Strip the "/query" text from the slash paragraph before applying.
			editor.update(() => {
				const node = getNodeByKey(paraKey) as { clear?: () => void; selectEnd?: () => void } | null;
				if (node && typeof node.clear === 'function') {
					node.clear();
					node.selectEnd?.();
				}
			});
		}
		applyBlock(action);
		editor.focus();
	}

	function moveMenu(delta: number) {
		const len = blockMenu.groups.flatMap((g) => g.items).length;
		if (!len) return;
		blockMenu = { ...blockMenu, active: (blockMenu.active + delta + len) % len };
	}
</script>

<div
	bind:this={containerEl}
	class={cn(
		'pp-rt relative rounded-[3px] border bg-white',
		invalid ? 'border-destructive' : 'border-stone-200'
	)}
>
	<div class="relative">
		{#if addBtn.visible || blockMenu.open || blockHandle.open}
			<!-- Six-dot block handle: move up / down / delete -->
			<button
				type="button"
				aria-label="Block actions"
				aria-expanded={blockHandle.open}
				class="absolute left-0 z-[3] flex h-6 w-5 items-center justify-center rounded-[3px] text-stone-300 transition-colors hover:bg-stone-100 hover:text-stone-600"
				style={`top:${addBtn.top + 2}px`}
				onmousedown={(e) => e.preventDefault()}
				onclick={toggleHandleMenu}
			>
				<svg width="10" height="16" viewBox="0 0 10 16" fill="currentColor" aria-hidden="true"><circle cx="3" cy="4" r="1.2" /><circle cx="7" cy="4" r="1.2" /><circle cx="3" cy="8" r="1.2" /><circle cx="7" cy="8" r="1.2" /><circle cx="3" cy="12" r="1.2" /><circle cx="7" cy="12" r="1.2" /></svg>
			</button>
			<!-- "+" inserter -->
			<button
				type="button"
				aria-label="Add block"
				aria-expanded={blockMenu.open && blockMenu.mode === 'add'}
				class="absolute left-5 z-[3] flex h-6 w-6 items-center justify-center rounded-[3px] text-stone-400 transition-colors hover:bg-stone-100 hover:text-stone-700"
				style={`top:${addBtn.top + 2}px`}
				onmousedown={(e) => e.preventDefault()}
				onclick={toggleAddMenu}
			>
				<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true">
					<path d="M12 5v14M5 12h14" stroke="currentColor" stroke-width="2" stroke-linecap="round" />
				</svg>
			</button>
		{/if}

		{#if blockHandle.open}
			<div
				class="pp-block-menu absolute left-1 z-[7] w-[150px] rounded-[4px] border border-stone-200 bg-white py-1.5"
				style={`top:${blockHandle.top + 30}px`}
				role="menu"
				tabindex="-1"
				aria-label="Block actions"
				onmousedown={(e) => e.preventDefault()}
			>
				<button type="button" role="menuitem" class="flex h-[30px] w-full items-center px-3 text-left text-[13px] text-stone-800 hover:bg-stone-50" onclick={() => moveBlock('up')}>Move up</button>
				<button type="button" role="menuitem" class="flex h-[30px] w-full items-center px-3 text-left text-[13px] text-stone-800 hover:bg-stone-50" onclick={() => moveBlock('down')}>Move down</button>
				<button type="button" role="menuitem" class="flex h-[30px] w-full items-center px-3 text-left text-[13px] text-stone-800 hover:bg-stone-50" onclick={deleteBlock}>Delete</button>
			</div>
		{/if}

		<div
			bind:this={rootEl}
			class="pp-editor-content min-h-[260px] py-4 pr-4 pl-12 text-sm leading-relaxed text-stone-800 outline-none"
			contenteditable="true"
			role="textbox"
			tabindex="0"
			aria-multiline="true"
			aria-label={ariaLabel}
			spellcheck="true"
		></div>
		{#if contentEmpty}
			<!-- Overlay uses the SAME padding + typography as the editor, so the
			     placeholder's first line lands exactly on the first caret line. -->
			<div
				class="pointer-events-none absolute inset-0 py-4 pr-4 pl-12 text-sm leading-relaxed text-stone-400 select-none"
				aria-hidden="true"
			>
				<!-- my-2 matches the .pp-p (0.5rem) margin so the text lands on the first caret line. -->
				<p class="my-2">Start typing, or press <span class="font-mono">/</span> for commands…</p>
			</div>
		{/if}
	</div>

	<!-- Floating selection toolbar (Payload inline-toolbar-popup) -->
	{#if floating.visible}
		<div
			bind:this={floatingEl}
			class="pp-inline-toolbar absolute z-[6] flex items-center gap-0.5 rounded-[4px] border border-stone-200 bg-white p-1"
			style={`left:${floating.left}px; top:${floating.top}px`}
			role="toolbar"
			tabindex="-1"
			aria-label="Formatting"
			onmousedown={(e) => e.preventDefault()}
		>
			<label class="sr-only" for="pp-block-type">Block type</label>
			<select
				id="pp-block-type"
				class="mr-0.5 h-6 rounded-[3px] border border-stone-200 bg-white px-1 text-[12px] text-stone-700 outline-none"
				value={blockType}
				onmousedown={(e) => e.stopPropagation()}
				onchange={onBlockSelect}
			>
				<option value="paragraph">Paragraph</option>
				<option value="h1">Heading 1</option>
				<option value="h2">Heading 2</option>
				<option value="h3">Heading 3</option>
				<option value="bullet">Unordered List</option>
				<option value="number">Ordered List</option>
				<option value="quote">Blockquote</option>
				<option value="code">Code block</option>
			</select>
			<span class="mx-0.5 h-4 w-px bg-stone-200" aria-hidden="true"></span>
			<button type="button" class="pp-fmt" aria-label="Bold" aria-pressed={isBold} onclick={() => format('bold')}>
				<span class="text-[13px] font-bold">B</span>
			</button>
			<button type="button" class="pp-fmt" aria-label="Italic" aria-pressed={isItalic} onclick={() => format('italic')}>
				<span class="font-serif text-[13px] italic">I</span>
			</button>
			<button type="button" class="pp-fmt" aria-label="Underline" aria-pressed={isUnderline} onclick={() => format('underline')}>
				<span class="text-[13px] underline">U</span>
			</button>
			<button type="button" class="pp-fmt" aria-label="Strikethrough" aria-pressed={isStrikethrough} onclick={() => format('strikethrough')}>
				<span class="text-[13px] line-through">S</span>
			</button>
			<button type="button" class="pp-fmt" aria-label="Subscript" aria-pressed={isSubscript} onclick={() => format('subscript')}>
				<span class="text-[12px]">X<sub class="text-[9px]">2</sub></span>
			</button>
			<button type="button" class="pp-fmt" aria-label="Superscript" aria-pressed={isSuperscript} onclick={() => format('superscript')}>
				<span class="text-[12px]">X<sup class="text-[9px]">2</sup></span>
			</button>
			<button type="button" class="pp-fmt" aria-label="Inline code" aria-pressed={isCode} onclick={() => format('code')}>
				<span class="font-mono text-[11px]">&lt;/&gt;</span>
			</button>
			<span class="mx-0.5 h-4 w-px bg-stone-200" aria-hidden="true"></span>
			<button type="button" class="pp-fmt" aria-label="Link" aria-pressed={isLink} onclick={editLink}>
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" class="h-4 w-4" aria-hidden="true">
					<path d="M10 13a5 5 0 0 0 7 0l3-3a5 5 0 0 0-7-7l-1 1" />
					<path d="M14 11a5 5 0 0 0-7 0l-3 3a5 5 0 0 0 7 7l1-1" />
				</svg>
			</button>
			<span class="pp-caret" class:pp-caret-below={floating.below} aria-hidden="true"></span>
		</div>
	{/if}

	<!-- Link editor popover (caret inside a link): URL + edit + remove -->
	{#if linkPopover.visible}
		<div
			class="pp-inline-toolbar absolute z-[6] flex items-center gap-1.5 rounded-[4px] border border-stone-200 bg-white px-2 py-1.5 text-[13px]"
			style={`left:${linkPopover.left}px; top:${linkPopover.top}px`}
			onmousedown={(e) => e.preventDefault()}
			role="toolbar"
			tabindex="-1"
			aria-label="Link"
		>
			<a
				href={linkPopover.url}
				target="_blank"
				rel="noopener noreferrer"
				class="max-w-[220px] truncate text-stone-600 underline underline-offset-2 hover:text-stone-900"
			>
				{linkPopover.url}
			</a>
			<button type="button" class="pp-fmt" aria-label="Edit link" onclick={editLink}>
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" class="h-4 w-4" aria-hidden="true"><path d="M12 20h9" /><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4Z" /></svg>
			</button>
			<button
				type="button"
				class="pp-fmt"
				aria-label="Remove link"
				onclick={() => editor.dispatchCommand(TOGGLE_LINK_COMMAND, null)}
			>
				<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" class="h-4 w-4" aria-hidden="true"><path d="M18.4 10.6 21 8a4 4 0 0 0-6-5l-2 2M5.6 13.4 3 16a4 4 0 0 0 6 5l2-2M4 4l16 16" /></svg>
			</button>
		</div>
	{/if}

	<!-- Block menu (Payload slash / add-block popup) -->
	{#if blockMenu.open}
		<div
			class="pp-block-menu absolute z-[7] w-[210px] overflow-y-auto rounded-[4px] border border-stone-200 bg-white py-1.5"
			style={`left:${blockMenu.left}px; top:${blockMenu.top}px; max-height:300px`}
			role="menu"
			tabindex="-1"
			aria-label="Insert block"
			onmousedown={(e) => e.preventDefault()}
		>
			{#each blockMenu.groups as group (group.title)}
				<p class="px-3 pb-0.5 pt-1 text-[10px] text-stone-400">{group.title}</p>
				{#each group.items as item (item.action)}
					{@const idx = flatMenu.findIndex((i) => i.action === item.action)}
					<button
						type="button"
						role="menuitem"
						class={cn(
							'flex h-[30px] w-full items-center gap-2 px-3 text-left text-[13px] text-stone-800',
							idx === blockMenu.active ? 'bg-stone-100' : 'hover:bg-stone-50'
						)}
						onmouseenter={() => (blockMenu = { ...blockMenu, active: idx })}
						onclick={() => chooseBlock(item.action)}
					>
						<span class="flex w-4 justify-center text-stone-400">
							{#if item.action === 'h1' || item.action === 'h2' || item.action === 'h3'}
								<span class="text-[11px] font-semibold">{item.action.toUpperCase()}</span>
							{:else if item.action === 'bullet'}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
							{:else if item.action === 'number'}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M10 6h11M10 12h11M10 18h11M4 4v4M3 8h2M4 14h1.5a1 1 0 0 1 .5 1.9L4 18h2" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" /></svg>
							{:else if item.action === 'quote'}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M6 17h3l2-4V7H5v6h3zm8 0h3l2-4V7h-6v6h3z" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round" /></svg>
							{:else if item.action === 'code'}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="m8 8-4 4 4 4m8-8 4 4-4 4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" /></svg>
							{:else if item.action === 'hr'}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M3 12h18" stroke="currentColor" stroke-width="2" stroke-linecap="round" /></svg>
							{:else if item.action === 'image'}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><rect x="3" y="3" width="18" height="18" rx="2" stroke="currentColor" stroke-width="1.6" /><circle cx="9" cy="9" r="2" stroke="currentColor" stroke-width="1.6" /><path d="m21 15-5-5L5 21" stroke="currentColor" stroke-width="1.6" /></svg>
							{:else}
								<svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden="true"><path d="M5 5h14M5 12h14M5 19h9" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" /></svg>
							{/if}
						</span>
						<span class="truncate">{item.label}</span>
					</button>
				{/each}
			{/each}
			{#if flatMenu.length === 0}
				<p class="px-3 py-2 text-[13px] text-stone-400">No blocks</p>
			{/if}
		</div>
	{/if}
</div>

<style>
	.pp-fmt {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		min-width: 24px;
		height: 24px;
		border-radius: 3px;
		color: var(--theme-elevation-600);
	}
	.pp-fmt:hover {
		background: var(--theme-elevation-100);
		color: var(--theme-elevation-900);
	}
	.pp-fmt[aria-pressed='true'] {
		background: var(--theme-elevation-200);
		color: var(--theme-elevation-900);
	}
	.pp-inline-toolbar {
		box-shadow:
			0 1px 2px 1px rgba(0, 0, 0, 0.05),
			0 4px 8px rgba(0, 0, 0, 0.1);
	}
	.pp-block-menu {
		box-shadow:
			0 1px 2px 1px rgba(0, 0, 0, 0.05),
			0 4px 8px rgba(0, 0, 0, 0.1);
	}
	.pp-caret {
		position: absolute;
		left: 20px;
		top: -8px;
		border: 8px solid transparent;
		border-bottom-color: #fff;
	}
	.pp-caret-below {
		top: auto;
		bottom: -8px;
		border-bottom-color: transparent;
		border-top-color: #fff;
	}
	:global(.pp-editor-content .pp-hr) {
		border: 0;
		border-top: 1px solid var(--theme-elevation-200);
		margin: 1rem 0;
	}
</style>

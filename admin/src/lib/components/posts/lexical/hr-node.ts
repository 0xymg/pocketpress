import {
	DecoratorNode,
	type DOMConversionMap,
	type DOMConversionOutput,
	type DOMExportOutput,
	type EditorConfig,
	type LexicalNode,
	type NodeKey,
	type SerializedLexicalNode
} from 'lexical';

/**
 * A minimal horizontal-rule block node. Lexical core ships no HR node (the
 * React package does), so this follows the same vanilla custom-node pattern as
 * ImageNode: the visible `<hr>` is built in `createDOM`, `decorate()` is a
 * no-op, and it round-trips through JSON and HTML export (`<hr>`, which the
 * server's sanitizer allowlist keeps).
 */
export type SerializedHorizontalRuleNode = SerializedLexicalNode;

export class HorizontalRuleNode extends DecoratorNode<null> {
	static getType(): string {
		return 'horizontalrule';
	}

	static clone(node: HorizontalRuleNode): HorizontalRuleNode {
		return new HorizontalRuleNode(node.__key);
	}

	constructor(key?: NodeKey) {
		super(key);
	}

	static importJSON(): HorizontalRuleNode {
		return new HorizontalRuleNode();
	}

	exportJSON(): SerializedHorizontalRuleNode {
		return { type: 'horizontalrule', version: 1 };
	}

	static importDOM(): DOMConversionMap | null {
		return {
			hr: () => ({ conversion: convertHorizontalRuleElement, priority: 0 })
		};
	}

	createDOM(_config: EditorConfig): HTMLElement {
		const el = document.createElement('hr');
		el.className = 'pp-hr';
		return el;
	}

	updateDOM(): boolean {
		return false;
	}

	exportDOM(): DOMExportOutput {
		return { element: document.createElement('hr') };
	}

	decorate(): null {
		return null;
	}

	isInline(): boolean {
		return false;
	}
}

function convertHorizontalRuleElement(): DOMConversionOutput {
	return { node: new HorizontalRuleNode() };
}

export function $createHorizontalRuleNode(): HorizontalRuleNode {
	return new HorizontalRuleNode();
}

export function $isHorizontalRuleNode(
	node: LexicalNode | null | undefined
): node is HorizontalRuleNode {
	return node instanceof HorizontalRuleNode;
}

import {
	DecoratorNode,
	type DOMConversionMap,
	type DOMConversionOutput,
	type DOMExportOutput,
	type EditorConfig,
	type LexicalNode,
	type NodeKey,
	type SerializedLexicalNode,
	type Spread
} from 'lexical';

/**
 * A minimal block image node. Lexical core ships no image node, so this is the
 * standard custom-node pattern — but without a framework decorator layer: the
 * visible `<img>` is built directly in `createDOM`, and `decorate()` is a no-op
 * (nothing calls it because we never mount React/Svelte decorators). That keeps
 * images working with a plain vanilla `createEditor` setup while still
 * round-tripping through JSON (`exportJSON`/`importJSON`) and HTML export
 * (`exportDOM` → `<img src alt width height>`, which the server's allowlist
 * keeps). Media is inserted by URL for now; the toolbar's `onInsertImage` hook
 * lets a later media browser supply `{ src, alt }` instead.
 */

export interface ImagePayload {
	src: string;
	alt?: string;
	width?: number;
	height?: number;
}

export type SerializedImageNode = Spread<
	{
		src: string;
		altText: string;
		width?: number;
		height?: number;
	},
	SerializedLexicalNode
>;

export class ImageNode extends DecoratorNode<null> {
	__src: string;
	__alt: string;
	__width?: number;
	__height?: number;

	static getType(): string {
		return 'image';
	}

	static clone(node: ImageNode): ImageNode {
		return new ImageNode(
			{ src: node.__src, alt: node.__alt, width: node.__width, height: node.__height },
			node.__key
		);
	}

	constructor(payload: ImagePayload, key?: NodeKey) {
		super(key);
		this.__src = payload.src;
		this.__alt = payload.alt ?? '';
		this.__width = payload.width;
		this.__height = payload.height;
	}

	static importJSON(serialized: SerializedImageNode): ImageNode {
		return new ImageNode({
			src: serialized.src,
			alt: serialized.altText,
			width: serialized.width,
			height: serialized.height
		});
	}

	exportJSON(): SerializedImageNode {
		return {
			type: 'image',
			version: 1,
			src: this.__src,
			altText: this.__alt,
			...(this.__width ? { width: this.__width } : {}),
			...(this.__height ? { height: this.__height } : {})
		};
	}

	static importDOM(): DOMConversionMap | null {
		return {
			img: () => ({ conversion: convertImageElement, priority: 0 })
		};
	}

	createDOM(_config: EditorConfig): HTMLElement {
		const wrapper = document.createElement('span');
		wrapper.className = 'pp-editor-image';
		const img = document.createElement('img');
		img.setAttribute('src', this.__src);
		img.setAttribute('alt', this.__alt);
		if (this.__width) img.setAttribute('width', String(this.__width));
		if (this.__height) img.setAttribute('height', String(this.__height));
		wrapper.appendChild(img);
		return wrapper;
	}

	updateDOM(): boolean {
		return false;
	}

	exportDOM(): DOMExportOutput {
		const element = document.createElement('img');
		element.setAttribute('src', this.__src);
		element.setAttribute('alt', this.__alt);
		if (this.__width) element.setAttribute('width', String(this.__width));
		if (this.__height) element.setAttribute('height', String(this.__height));
		return { element };
	}

	decorate(): null {
		return null;
	}

	isInline(): boolean {
		return false;
	}
}

function convertImageElement(el: HTMLElement): DOMConversionOutput | null {
	if (!(el instanceof HTMLImageElement)) return null;
	const { src, alt, width, height } = el;
	if (!src) return null;
	return {
		node: new ImageNode({ src, alt, width: width || undefined, height: height || undefined })
	};
}

export function $createImageNode(payload: ImagePayload): ImageNode {
	return new ImageNode(payload);
}

export function $isImageNode(node: LexicalNode | null | undefined): node is ImageNode {
	return node instanceof ImageNode;
}

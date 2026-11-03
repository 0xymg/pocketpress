import { render, screen, waitFor } from '@testing-library/svelte';
import { afterEach, describe, expect, it, vi } from 'vitest';
import LexicalEditor from './LexicalEditor.svelte';

/**
 * jsdom-level coverage. Lexical drives a contenteditable through the DOM APIs
 * jsdom provides, so mount, serialization, HTML export and the placeholder are
 * exercisable. The Payload-style UI (floating selection toolbar, "+" inserter,
 * "/" slash menu) is driven by a *live* RangeSelection and viewport geometry,
 * neither of which jsdom reproduces — so that surface is covered by the pure
 * model (editor-model.test.ts) and by the Playwright editor drive in
 * e2e/.tmp / the golden path, not here.
 */

const PARAGRAPH_JSON = JSON.stringify({
	root: {
		children: [
			{
				children: [
					{
						detail: 0,
						format: 0,
						mode: 'normal',
						style: '',
						text: 'Hello Lexical',
						type: 'text',
						version: 1
					}
				],
				direction: 'ltr',
				format: '',
				indent: 0,
				type: 'paragraph',
				version: 1
			}
		],
		direction: 'ltr',
		format: '',
		indent: 0,
		type: 'root',
		version: 1
	}
});

afterEach(() => {
	vi.restoreAllMocks();
});

describe('LexicalEditor', () => {
	it('mounts and reports an initial serialization, with no fixed toolbar', async () => {
		const onReady = vi.fn();
		render(LexicalEditor, { props: { onReady } });

		await waitFor(() => expect(onReady).toHaveBeenCalled());

		const [json, html] = onReady.mock.calls[0];
		expect(typeof json).toBe('string');
		expect(json).toContain('"root"');
		expect(typeof html).toBe('string');

		// The editing surface exists and is an accessible textbox…
		expect(screen.getByRole('textbox', { name: 'Content' })).toBeInTheDocument();
		// …and there is no always-on fixed formatting toolbar (Payload shows a
		// floating one only on selection).
		expect(screen.queryByRole('toolbar', { name: 'Formatting' })).not.toBeInTheDocument();
	});

	it('loads content from JSON and round-trips it to HTML', async () => {
		const onReady = vi.fn();
		render(LexicalEditor, { props: { initialJson: PARAGRAPH_JSON, onReady } });

		await waitFor(() => expect(onReady).toHaveBeenCalled());

		const [, html] = onReady.mock.calls[0];
		expect(html).toContain('Hello Lexical');
		expect(screen.getByRole('textbox')).toHaveTextContent('Hello Lexical');
	});

	it('falls back to an empty editor for invalid JSON and shows the placeholder hint', async () => {
		const onReady = vi.fn();
		render(LexicalEditor, { props: { initialJson: '{not valid json', onReady } });

		await waitFor(() => expect(onReady).toHaveBeenCalled());
		expect(screen.getByText(/Start typing/)).toBeInTheDocument();
		// The placeholder advertises the slash-command affordance.
		expect(screen.getByText('/')).toBeInTheDocument();
	});

	it('exposes the editing surface via the given accessible name', async () => {
		const onReady = vi.fn();
		render(LexicalEditor, { props: { ariaLabel: 'Body', onReady } });
		await waitFor(() => expect(onReady).toHaveBeenCalled());
		expect(screen.getByRole('textbox', { name: 'Body' })).toBeInTheDocument();
	});
});

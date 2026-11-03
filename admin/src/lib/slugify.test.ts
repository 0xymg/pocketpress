import { describe, expect, it } from 'vitest';
import { slugify } from './slugify';

describe('slugify', () => {
	it('lowercases and hyphenates whitespace-separated words', () => {
		expect(slugify('Hello World')).toBe('hello-world');
	});

	it('strips punctuation and collapses runs of separators', () => {
		expect(slugify('  Hello, World!!  ')).toBe('hello-world');
	});

	it('strips general Latin diacritics', () => {
		expect(slugify('Café Déjà Vu')).toBe('cafe-deja-vu');
	});

	it('maps Turkish diacritics, including dotless ı and dotted İ', () => {
		expect(slugify('Merhaba Dünya: İş Güç ıçöşü')).toBe('merhaba-dunya-is-guc-icosu');
	});

	it('trims leading and trailing hyphens', () => {
		expect(slugify('---already---slugged---')).toBe('already-slugged');
	});

	it('returns an empty string for input with no alphanumeric content', () => {
		expect(slugify('!!!')).toBe('');
	});
});

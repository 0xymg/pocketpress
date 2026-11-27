import { describe, expect, it } from 'vitest';

import { activeKeyFromPath, isEditorPath, NAV_GROUPS, type NavKey } from './nav';

describe('activeKeyFromPath', () => {
	const cases: [string, NavKey | null][] = [
		['/', 'dashboard'],
		['/posts', 'posts'],
		['/posts/new', 'posts'],
		['/posts/abc-123', 'posts'],
		['/calendar', 'calendar'],
		['/reviews', 'reviews'],
		['/categories', 'categories'],
		['/tags', 'tags'],
		['/media', 'media'],
		['/settings/profile', 'profile'],
		['/settings/users', 'users'],
		['/settings/general', 'settings'],
		['/settings/webhooks', 'webhooks'],
		['/settings/api-keys', 'api-keys'],
		['/login', null],
		['/setup', null],
		['/nonexistent', null]
	];

	it.each(cases)('maps %s -> %s', (pathname, expected) => {
		expect(activeKeyFromPath(pathname)).toBe(expected);
	});
});

describe('isEditorPath', () => {
	it('is true for the new/edit editor routes but not the list', () => {
		expect(isEditorPath('/posts/new')).toBe(true);
		expect(isEditorPath('/posts/abc-123')).toBe(true);
		expect(isEditorPath('/posts')).toBe(false);
		expect(isEditorPath('/')).toBe(false);
	});
});

describe('NAV_GROUPS', () => {
	it('exposes a public Content group and an owner-only System group', () => {
		const content = NAV_GROUPS.find((g) => g.label === 'Content');
		const system = NAV_GROUPS.find((g) => g.label === 'System');

		expect(content).toBeDefined();
		expect(content?.ownerOnly).toBeFalsy();
		expect(content?.items.map((i) => i.key)).toEqual([
			'dashboard',
			'posts',
			'calendar',
			'reviews',
			'categories',
			'tags',
			'media',
			'profile'
		]);

		expect(system).toBeDefined();
		expect(system?.ownerOnly).toBe(true);
		expect(system?.items.map((i) => i.key)).toEqual(['users', 'settings', 'webhooks', 'api-keys']);
	});

	it('gives every item a unique key and a leading-slash href', () => {
		const items = NAV_GROUPS.flatMap((g) => g.items);
		const keys = items.map((i) => i.key);
		expect(new Set(keys).size).toBe(keys.length);
		for (const item of items) {
			expect(item.href.startsWith('/')).toBe(true);
			expect(item.label.length).toBeGreaterThan(0);
		}
	});
});

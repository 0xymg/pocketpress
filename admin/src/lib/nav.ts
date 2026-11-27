/**
 * Navigation model for the admin sidebar (Sidebar.svelte / AppShell.svelte).
 *
 * Kept as a plain, framework-free module so the item/group data and the
 * active-route matching can be unit tested without rendering Svelte or mocking
 * $app/state. The sidebar reads the current pathname from $app/state in exactly
 * one place (the root layout) and passes it down; `activeKeyFromPath` below is
 * the single, pure source of truth for which item is highlighted.
 */

export type NavKey =
	| 'dashboard'
	| 'posts'
	| 'calendar'
	| 'categories'
	| 'tags'
	| 'media'
	| 'profile'
	| 'users'
	| 'settings'
	| 'webhooks'
	| 'api-keys';

export interface NavItem {
	key: NavKey;
	href: string;
	label: string;
}

export interface NavGroup {
	/** Small muted heading shown above the group's items. */
	label: string;
	/** When true the whole group is hidden from non-owner (editor) accounts. */
	ownerOnly?: boolean;
	items: NavItem[];
}

/**
 * The sidebar's two groups, Payload-style: a primary content group and an
 * owner-only system group. Accessible link names ("Dashboard", "Posts",
 * "Categories", "Tags", "Media", "Users", "Settings", "Webhooks") are relied on
 * by the e2e golden path — keep them stable.
 */
export const NAV_GROUPS: NavGroup[] = [
	{
		label: 'Content',
		items: [
			{ key: 'dashboard', href: '/', label: 'Dashboard' },
			{ key: 'posts', href: '/posts', label: 'Posts' },
			{ key: 'calendar', href: '/calendar', label: 'Calendar' },
			{ key: 'categories', href: '/categories', label: 'Categories' },
			{ key: 'tags', href: '/tags', label: 'Tags' },
			{ key: 'media', href: '/media', label: 'Media' },
			{ key: 'profile', href: '/settings/profile', label: 'Profile' }
		]
	},
	{
		label: 'System',
		ownerOnly: true,
		items: [
			{ key: 'users', href: '/settings/users', label: 'Users' },
			{ key: 'settings', href: '/settings/general', label: 'Settings' },
			{ key: 'webhooks', href: '/settings/webhooks', label: 'Webhooks' },
			{ key: 'api-keys', href: '/settings/api-keys', label: 'API keys' }
		]
	}
];

/**
 * Maps a route pathname to the nav item that should read as active. Section
 * routes (e.g. /posts/new, /posts/{id}) all light up their parent item. Routes
 * with no sidebar entry (/, /login, /setup handled by the guard) return null.
 */
export function activeKeyFromPath(pathname: string): NavKey | null {
	if (pathname === '/') return 'dashboard';
	if (pathname.startsWith('/posts')) return 'posts';
	if (pathname.startsWith('/calendar')) return 'calendar';
	if (pathname.startsWith('/categories')) return 'categories';
	if (pathname.startsWith('/tags')) return 'tags';
	if (pathname.startsWith('/media')) return 'media';
	if (pathname.startsWith('/settings/profile')) return 'profile';
	if (pathname.startsWith('/settings/users')) return 'users';
	if (pathname.startsWith('/settings/general')) return 'settings';
	if (pathname.startsWith('/settings/webhooks')) return 'webhooks';
	if (pathname.startsWith('/settings/api-keys')) return 'api-keys';
	return null;
}

export interface Crumb {
	label: string;
	/** Present for all but the last (current) crumb. */
	href?: string;
}

/**
 * Breadcrumb ("step nav") segments shown in the top app bar, mirroring
 * Payload's `< logo > / Collection / Document` trail. The last crumb is the
 * current page (no href).
 */
export function breadcrumbFromPath(pathname: string): Crumb[] {
	if (pathname === '/posts/new') return [{ label: 'Posts', href: '/posts' }, { label: 'Create New' }];
	if (pathname.startsWith('/posts/')) return [{ label: 'Posts', href: '/posts' }, { label: 'Edit' }];

	const key = activeKeyFromPath(pathname);
	const item = NAV_GROUPS.flatMap((g) => g.items).find((i) => i.key === key);
	if (item) return [{ label: item.label }];
	return [{ label: 'Dashboard' }];
}

/**
 * The post editor (/posts/new, /posts/{id}) manages its own full-bleed layout
 * and header, so the shell renders it without the centered content wrapper the
 * other pages get. The /posts list itself is a normal page.
 */
export function isEditorPath(pathname: string): boolean {
	return pathname.startsWith('/posts/');
}

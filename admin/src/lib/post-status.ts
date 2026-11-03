import type { PostStatus } from './api';

/**
 * Shared status-badge styling for posts (Phase 6 — plan section 9: "Status
 * badge" on the post list, and the read-only status Badge that replaced the
 * status <select> in the editor). Kept as plain Tailwind utility classes
 * layered on the Badge component's `outline` variant via `cn()`/twMerge, one
 * source of truth so the list and the editor never drift apart.
 *
 * Colors: draft=stone, scheduled=amber, published=teal, archived=gray —
 * distinct at a glance, no reliance on text alone.
 */
export const POST_STATUS_BADGE_CLASSES: Record<PostStatus, string> = {
	draft: 'border-stone-300 bg-stone-100 text-stone-700',
	scheduled: 'border-amber-300 bg-amber-100 text-amber-800',
	published: 'border-teal-300 bg-teal-100 text-teal-800',
	archived: 'border-gray-300 bg-gray-100 text-gray-600'
};

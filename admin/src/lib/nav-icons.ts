/**
 * Hugeicons (free stroke-rounded) mapped to sidebar nav keys.
 *
 * Animate UI's Hugeicons docs use this same free set at stroke width 2; their
 * React/Motion icon components are adapted in AnimateHugeicon.svelte for Svelte.
 */
import type { IconSvgElement } from '@hugeicons/svelte';
import {
	Calendar03Icon,
	CheckmarkBadge01Icon,
	DashboardSquare01Icon,
	File01Icon,
	Folder01Icon,
	Image01Icon,
	Key01Icon,
	LayoutTemplateIcon,
	Settings02Icon,
	Tag01Icon,
	UserIcon,
	UserMultiple02Icon,
	WebhookIcon
} from '@hugeicons/core-free-icons';
import type { NavKey } from '$lib/nav';

export const NAV_ICONS: Record<NavKey, IconSvgElement> = {
	dashboard: DashboardSquare01Icon,
	posts: File01Icon,
	calendar: Calendar03Icon,
	reviews: CheckmarkBadge01Icon,
	templates: LayoutTemplateIcon,
	categories: Folder01Icon,
	tags: Tag01Icon,
	media: Image01Icon,
	profile: UserIcon,
	users: UserMultiple02Icon,
	settings: Settings02Icon,
	webhooks: WebhookIcon,
	'api-keys': Key01Icon
};

/** Stroke width used by Animate UI's Hugeicons free set. */
export const NAV_ICON_STROKE_WIDTH = 2;

/** Compact size that sits beside 13px sidebar labels. */
export const NAV_ICON_SIZE = 16;

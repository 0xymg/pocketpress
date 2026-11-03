import type { DeliveryStatus } from './api';

/**
 * Shared status-badge styling for webhook deliveries (Phase 7 — plan
 * sections 8/9/13: pending=amber, success=teal, failed=red). Mirrors
 * post-status.ts's approach: plain Tailwind utility classes layered on the
 * Badge component's `outline` variant via `cn()`/twMerge.
 */
export const DELIVERY_STATUS_BADGE_CLASSES: Record<DeliveryStatus, string> = {
	pending: 'border-amber-300 bg-amber-100 text-amber-800',
	success: 'border-teal-300 bg-teal-100 text-teal-800',
	failed: 'border-red-300 bg-red-100 text-red-800'
};

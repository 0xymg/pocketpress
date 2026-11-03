import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]) {
	return twMerge(clsx(inputs));
}

export type WithoutChild<T> = T extends { child?: unknown } ? Omit<T, 'child'> : T;
export type WithoutChildren<T> = T extends { children?: unknown } ? Omit<T, 'children'> : T;
export type WithoutChildrenOrChild<T> = WithoutChildren<WithoutChild<T>>;
export type WithElementRef<T, U extends HTMLElement = HTMLElement> = T & { ref?: U | null };

/** Formats a byte count for display in the media library (e.g. "482 KB", "3.1 MB"). */
export function formatBytes(bytes: number): string {
	if (!Number.isFinite(bytes) || bytes < 1024) return `${Math.max(0, bytes)} B`;
	const units = ['KB', 'MB', 'GB'];
	let value = bytes / 1024;
	let unitIndex = 0;
	while (value >= 1024 && unitIndex < units.length - 1) {
		value /= 1024;
		unitIndex += 1;
	}
	return `${value.toFixed(value < 10 ? 1 : 0)} ${units[unitIndex]}`;
}

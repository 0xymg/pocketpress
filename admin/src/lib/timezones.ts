/**
 * A short list of common IANA timezone identifiers for the first-run setup
 * wizard's timezone <select>. Not exhaustive — just enough coverage that
 * most owners find their region without scrolling through all ~400 IANA
 * zones, plus whatever the browser reports as the local zone.
 */
const COMMON_TIMEZONES = [
	'UTC',
	'America/New_York',
	'America/Chicago',
	'America/Denver',
	'America/Los_Angeles',
	'America/Sao_Paulo',
	'Europe/London',
	'Europe/Paris',
	'Europe/Berlin',
	'Europe/Madrid',
	'Europe/Moscow',
	'Africa/Cairo',
	'Africa/Johannesburg',
	'Asia/Istanbul',
	'Asia/Dubai',
	'Asia/Kolkata',
	'Asia/Shanghai',
	'Asia/Tokyo',
	'Asia/Seoul',
	'Asia/Singapore',
	'Australia/Sydney',
	'Pacific/Auckland'
];

/** Best-effort detection of the browser's local IANA timezone. */
export function detectTimezone(): string {
	try {
		return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
	} catch {
		return 'UTC';
	}
}

/** Common timezones plus the detected local zone, deduplicated and sorted. */
export function buildTimezoneOptions(): string[] {
	const zones = new Set(COMMON_TIMEZONES);
	zones.add(detectTimezone());
	return Array.from(zones).sort();
}

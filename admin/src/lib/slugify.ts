/**
 * Client-side slug suggestion, mirroring (loosely) the server's own slug
 * normalization. The server remains the authority — this only drives the
 * auto-suggest while a post is new and its slug field hasn't been touched
 * (plan section 9: "Generate slug from title only when slug is empty" /
 * "Never overwrite a manually edited slug").
 */

// Turkish letters that don't decompose into ASCII + a combining mark under
// NFD normalization (dotless ı / dotted İ have no canonical decomposition),
// so they need an explicit mapping. ğ, ş, ç, ö, ü *do* decompose via NFD,
// but are listed explicitly too so the mapping stays obvious and doesn't
// silently depend on normalization behavior.
const CHAR_MAP: Record<string, string> = {
	ı: 'i',
	İ: 'i',
	ğ: 'g',
	Ğ: 'g',
	ş: 's',
	Ş: 's',
	ç: 'c',
	Ç: 'c',
	ö: 'o',
	Ö: 'o',
	ü: 'u',
	Ü: 'u'
};

/**
 * Turns free text into a URL-safe slug: lowercase, diacritics stripped
 * (including Turkish ı/ş/ğ/ç/ö/ü), non-alphanumeric runs collapsed to a
 * single hyphen, and leading/trailing hyphens trimmed.
 */
export function slugify(input: string): string {
	let result = input.replace(/[İıĞğŞşÇçÖöÜü]/g, (char) => CHAR_MAP[char] ?? char);
	result = result.toLowerCase();
	result = result.normalize('NFD').replace(/[\u0300-\u036f]/g, '');
	result = result.replace(/[^a-z0-9]+/g, '-');
	result = result.replace(/^-+|-+$/g, '');
	return result;
}

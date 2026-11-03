import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vitest/config';

// https://svelte.dev/docs/kit/single-page-apps — pure client-side SPA:
// ssr/prerender are disabled globally in src/routes/+layout.ts, and
// adapter-static's `fallback` serves index.html for every path so the
// Go backend's own SPA fallback just needs to hand off "/" and unknown
// paths to this one file.
export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({
				// Build output must land in admin/dist — internal/http embeds this
				// directory via admin/embed.go (//go:embed all:dist).
				pages: 'dist',
				assets: 'dist',
				fallback: 'index.html'
			})
		})
	],
	server: {
		proxy: {
			'/healthz': {
				target: 'http://127.0.0.1:8090',
				changeOrigin: true
			},
			'/api': {
				target: 'http://127.0.0.1:8090',
				changeOrigin: true
			}
		}
	},
	// Vitest executes test files in Node, so Vite's default module resolution
	// picks the server/SSR export condition for packages like svelte — even
	// with test.environment set to "jsdom" — which breaks `mount()`. Forcing
	// the "browser" condition under Vitest fixes it.
	resolve: process.env.VITEST ? { conditions: ['browser'] } : undefined,
	test: {
		environment: 'jsdom',
		setupFiles: ['./src/test/setup.ts'],
		css: true,
		include: ['src/**/*.{test,spec}.{js,ts}']
	}
});

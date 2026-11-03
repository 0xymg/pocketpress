// PocketPress admin is a pure client-side SPA: the Go backend has no SSR
// story for it, it just serves the built dist/index.html and static assets
// (see admin/embed.go and internal/http). Disabling ssr/prerender here,
// at the root layout, applies to every route.
export const ssr = false;
export const prerender = false;

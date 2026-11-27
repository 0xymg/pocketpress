# CLAUDE.md — Instructions for Claude Code

This file documents standing rules for AI agents working in this repository. The authoritative specification is **PocketPress_Development_Plan.md**.

## Core context

You are implementing **PocketPress**, a tiny self-hosted blog backend for product developers.

**Always read PocketPress_Development_Plan.md completely before modifying files.**

## Product rule

PocketPress is **not a website builder** and **not a generic CMS**. It provides a writing dashboard and a stable headless content API for an existing product website.

Features that serve newsletters, commerce, community, memberships, or help build the website itself are out of scope.

## Architecture rule

- PocketPress is a **fully standalone Go application** — no embedded framework underneath it.
- HTTP layer: standard library **net/http** with Go 1.22+ **ServeMux** patterns and custom small middleware.
- CLI: **spf13/cobra**.
- Persistence: **SQLite via modernc.org/sqlite** (pure Go, CGO-free, single static binary).
- Schema: plain **SQL migrations embedded in the binary**, applied by a small custom migration runner.
- Auth: custom **argon2id** password hashing, server-side sessions stored in SQLite, secure HTTP-only cookies.
- All external integrations must use the custom versioned **/api/v1 API**; internal storage schema is never exposed directly.

## Working rules

1. **Implement only the requested phase.** Do not skip ahead or implement future phases.
2. **Do not add features listed under "Explicitly excluded from v0.1"** in the development plan.
3. **Use SQL migrations for every schema change.** Never create production schema manually against the live database.
4. **Keep domain logic out of HTTP handlers and UI components.** Handlers validate input, call a service, serialize a response.
5. **Enforce permissions and validation on the server.** Never trust the client.
6. **Add tests for each implemented behavior.** Test happy path, error cases, and permission boundaries.
7. **Never store raw API keys or webhook secrets.** Hash or encrypt all sensitive data.
8. **Do not expose stack traces, internal collection names, or raw database errors** in responses.
9. **Keep the application runnable after every phase.** No broken intermediate states.
10. **Prefer clear, boring, and maintainable code over abstractions without a current use.** Avoid over-engineering.
11. **Do not silently change an existing API response contract.** If the schema changes, version it.
12. **Update the OpenAPI spec whenever public API behavior changes.**
13. **Run formatting, tests, and builds before declaring a phase complete.**

## Phase checklist

### At the start of each phase

- [ ] Inspect the existing repository and its current state
- [ ] Summarize the implementation approach for this phase
- [ ] List the files you expect to create or modify
- [ ] Identify any conflict with the development plan
- [ ] Confirm the previous phase's acceptance criteria are still satisfied

### At the end of each phase

- [ ] Report what was implemented
- [ ] List all changed files
- [ ] Document commands to run/test the feature
- [ ] Run all tests and confirm they pass
- [ ] Run the build and confirm it succeeds
- [ ] Report any unresolved risks or deviations from the plan
- [ ] Describe the next phase (do not implement it yet)

## Current phase

**v0.2 in progress** — slices so far: RSS/Atom feeds (`GET /api/v1/feed.rss`, `GET /api/v1/feed.atom`), Sitemap (`GET /api/v1/sitemap.xml`), JavaScript/TypeScript SDK (`sdk/`, package `@pocketpress/sdk`), Import from Markdown (`POST /api/admin/v1/posts/import/markdown`), and Import from Ghost/WordPress (`POST /api/admin/v1/posts/import/ghost`, `POST /api/admin/v1/posts/import/wordpress`).

v0.1 MVP remains complete (all eight development-plan phases verified as of 2026-08-02). Remaining v0.2 items from plan §16 (do not skip ahead within a PR): FTS, image transforms, webhook filters, API key management.

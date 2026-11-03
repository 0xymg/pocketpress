package content

import "context"

// Events is the content service's outbound notification seam: it is called
// after a post's publish state changes, so a later subsystem can react
// without the service knowing how. Phase 6 only defines the seam and fires a
// no-op default; Phase 7 replaces the default with the webhook dispatcher.
//
// Every method receives the fully-hydrated post aggregate as it exists after
// the change. Implementations must not block the request (Phase 7's
// dispatcher enqueues and returns); the service calls them synchronously
// after the storage write commits.
//
// Which change fires which event:
//
//   - PostPublished   — a post transitions into the published status, via the
//     publish operation or the scheduled-publish worker's promotion. A no-op
//     re-publish of an already-published post does not fire it.
//   - PostUnpublished — a currently-published post is moved back to draft.
//     Unpublishing a scheduled or archived post (never publicly live as
//     "published") does not fire it.
//   - PostUpdated     — a published post's content/metadata is edited via the
//     normal update. Editing a non-published post does not fire it.
//   - PostDeleted     — a published post is deleted. Deleting a non-published
//     post does not fire it.
type Events interface {
	PostPublished(ctx context.Context, p *Post)
	PostUnpublished(ctx context.Context, p *Post)
	PostUpdated(ctx context.Context, p *Post)
	PostDeleted(ctx context.Context, p *Post)
}

// NoopEvents is the default Events implementation: it drops every event. It
// is what a Service uses until SetEvents installs a real sink (Phase 7).
type NoopEvents struct{}

func (NoopEvents) PostPublished(context.Context, *Post)   {}
func (NoopEvents) PostUnpublished(context.Context, *Post) {}
func (NoopEvents) PostUpdated(context.Context, *Post)     {}
func (NoopEvents) PostDeleted(context.Context, *Post)     {}

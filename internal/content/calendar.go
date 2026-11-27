package content

import (
	"context"
	"fmt"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// MaxCalendarRange is the widest from→to window the calendar endpoint
// accepts. A typical month grid with leading/trailing week days fits well
// under this; wider ranges would need pagination (out of scope for v0.3).
const MaxCalendarRange = 62 * 24 * time.Hour

// CalendarEntry is one post placed on the admin scheduling calendar.
// At is the placement timestamp: scheduled_for for scheduled posts,
// published_at for published posts.
type CalendarEntry struct {
	Post Post
	At   time.Time
}

// ListCalendar returns scheduled and published posts whose placement time
// falls in the inclusive [from, to] window, hydrated with authors.
// Returns a ValidationError when from/to are inverted or the span exceeds
// MaxCalendarRange.
func (s *Service) ListCalendar(ctx context.Context, from, to time.Time) ([]CalendarEntry, error) {
	from = from.UTC()
	to = to.UTC()
	if to.Before(from) {
		return nil, &ValidationError{Fields: map[string]string{
			"to": "End of range must be on or after the start.",
		}}
	}
	if to.Sub(from) > MaxCalendarRange {
		return nil, &ValidationError{Fields: map[string]string{
			"to": "Calendar range must be at most 62 days.",
		}}
	}

	rows, err := store.ListCalendarPosts(ctx, s.db, from, to)
	if err != nil {
		return nil, err
	}

	authorCache := map[string]Author{}
	entries := make([]CalendarEntry, 0, len(rows))
	for _, row := range rows {
		at, ok := calendarAt(row)
		if !ok {
			continue
		}
		p, err := s.hydrate(ctx, row, authorCache)
		if err != nil {
			return nil, fmt.Errorf("hydrate calendar post %s: %w", row.ID, err)
		}
		entries = append(entries, CalendarEntry{Post: p, At: at})
	}
	return entries, nil
}

// calendarAt returns the timestamp used to place a post on the calendar.
func calendarAt(p store.Post) (time.Time, bool) {
	switch p.Status {
	case store.PostStatusScheduled:
		if p.ScheduledFor != nil {
			return p.ScheduledFor.UTC(), true
		}
	case store.PostStatusPublished:
		if p.PublishedAt != nil {
			return p.PublishedAt.UTC(), true
		}
	}
	return time.Time{}, false
}

package adminapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/http/middleware"
)

// calendarEntryDTO is one post chip on the admin scheduling calendar.
type calendarEntryDTO struct {
	ID           string     `json:"id"`
	Title        string     `json:"title"`
	Slug         string     `json:"slug"`
	Status       string     `json:"status"`
	Locale       string     `json:"locale"`
	Author       authorDTO  `json:"author"`
	At           time.Time  `json:"at"`
	ScheduledFor *time.Time `json:"scheduled_for"`
	PublishedAt  *time.Time `json:"published_at"`
}

// listCalendar (GET /calendar) returns scheduled and published posts whose
// placement time falls in the inclusive [from, to] window. Query params:
//
//	from, to — required RFC3339 timestamps or YYYY-MM-DD dates (UTC).
//	          A date-only `to` includes the whole UTC day.
func (h *Handler) listCalendar(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	from, fromErr := parseCalendarBound(q.Get("from"), false)
	to, toErr := parseCalendarBound(q.Get("to"), true)
	fields := map[string]string{}
	if fromErr != "" {
		fields["from"] = fromErr
	}
	if toErr != "" {
		fields["to"] = toErr
	}
	if len(fields) > 0 {
		h.validationError(w, fields)
		return
	}

	entries, err := h.content.ListCalendar(r.Context(), from, to)
	if err != nil {
		h.handleContentError(w, err, "Not found.")
		return
	}

	out := make([]calendarEntryDTO, 0, len(entries))
	for _, e := range entries {
		locale := e.Post.Row.Locale
		if locale == "" {
			locale = "en"
		}
		out = append(out, calendarEntryDTO{
			ID:     e.Post.Row.ID,
			Title:  e.Post.Row.Title,
			Slug:   e.Post.Row.Slug,
			Status: e.Post.Row.Status,
			Locale: locale,
			Author: authorDTO{
				ID:   e.Post.Author.ID,
				Name: e.Post.Author.Name,
				Slug: e.Post.Author.Slug,
			},
			At:           e.At,
			ScheduledFor: e.Post.Row.ScheduledFor,
			PublishedAt:  e.Post.Row.PublishedAt,
		})
	}

	middleware.WriteJSON(w, http.StatusOK, map[string]any{"entries": out})
}

// parseCalendarBound parses a from/to query value. When endOfDay is true and
// the value is a bare date (YYYY-MM-DD), the result is that day's last UTC
// second so an inclusive date range covers the whole day.
func parseCalendarBound(raw string, endOfDay bool) (time.Time, string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, "Required."
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), ""
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		t = t.UTC()
		if endOfDay {
			t = t.Add(24*time.Hour - time.Second)
		}
		return t, ""
	}
	return time.Time{}, "Must be an RFC3339 timestamp or YYYY-MM-DD date."
}

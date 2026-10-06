package dto

import "time"

// MaxListLimit caps todo list responses.
const MaxListLimit = 200

// ListFilters are combinable AND-filters for listing todos.
type ListFilters struct {
	Keyword string
	Status  string
	DueFrom *time.Time
	DueTo   *time.Time
	NoDue   bool
	// CompletedSince is an inclusive completion instant, applied before the
	// list limit so recent completions cannot be hidden by older rows.
	CompletedSince *time.Time
}

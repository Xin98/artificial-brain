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
}

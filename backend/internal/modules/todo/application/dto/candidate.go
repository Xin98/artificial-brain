package dto

import "time"

// MaxCandidateLimit caps candidate searches at 11 so callers can detect
// "too many matches, refine the request" (>10).
const MaxCandidateLimit = 11

// Candidate is a pending todo matched by a delete-intent search; it carries
// the current Version so confirmations can re-check it.
type Candidate struct {
	TodoID   string     `json:"todoId"`
	Title    string     `json:"title"`
	DueAtUTC *time.Time `json:"dueAtUtc,omitempty"`
	Version  int        `json:"version"`
}

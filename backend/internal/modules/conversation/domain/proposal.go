// Package domain holds the Conversation context's aggregates and invariants.
package domain

import "time"

// Intent identifies the operation a proposal asks for. Only the registered
// intents are dispatchable; anything else is unsupported.
type Intent string

const (
	IntentTodoCreate Intent = "todo.create"
	IntentTodoDelete Intent = "todo.delete"
	IntentTodoList   Intent = "todo.list"
	IntentUnknown    Intent = "unknown"
)

// IntentProposal is the only shape in which model output reaches the router:
// it is produced exclusively by the application validation choke point.
type IntentProposal struct {
	Intent        Intent
	Confidence    float64
	MissingFields []string
	Arguments     ProposalArguments
}

// ProposalArguments carries the validated, per-intent arguments. Fields not
// relevant to the proposal's intent stay zero-valued.
type ProposalArguments struct {
	// todo.create
	Title       string
	Description *string
	DueAtUTC    *time.Time
	Timezone    string

	// todo.delete / todo.list
	Keyword string

	// todo.list filters
	Status  string
	DueFrom *time.Time
	DueTo   *time.Time
	NoDue   bool
}

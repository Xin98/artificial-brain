package domain

// ModelTurn is the validated outcome of one unified model call: the
// natural-language reply plus an optional structured intent proposal. A nil
// Proposal marks a pure chat turn; a present proposal has already passed the
// v1 proposal validation choke point.
type ModelTurn struct {
	Reply    string
	Proposal *IntentProposal
}

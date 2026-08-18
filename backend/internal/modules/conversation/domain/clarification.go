package domain

// ClarificationReason explains why the conversation asks for more
// information instead of guessing.
type ClarificationReason string

const (
	// ReasonMissingFields marks required information the model flagged as absent.
	ReasonMissingFields ClarificationReason = "missing_fields"
	// ReasonLowConfidence marks proposals below the dispatch confidence floor.
	ReasonLowConfidence ClarificationReason = "low_confidence"
	// ReasonAmbiguousCandidates marks searches with too many candidates to pick from.
	ReasonAmbiguousCandidates ClarificationReason = "ambiguous_candidates"
)

// Clarification requests missing information from the user; it never guesses.
type Clarification struct {
	MissingFields []string
	Reason        ClarificationReason
}

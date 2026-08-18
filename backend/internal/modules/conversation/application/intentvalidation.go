// Package application implements the Conversation use cases. ValidateProposal
// is the single runtime choke point every model output must pass before it
// can reach the Todo context; invalid proposals never become writes.
package application

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

// MaxKeywordLength bounds candidate search keywords.
const MaxKeywordLength = 100

// Mirrors of Todo's public contract bounds. Conversation may only import
// Todo's application dto types (D6), so the numeric and enum bounds are
// repeated here and pinned by the OpenAPI contracts.
const (
	maxTitleLength  = 200
	statusPending   = "pending"
	statusCompleted = "completed"
)

// MinDispatchConfidence is the floor below which a proposal is clarified
// instead of dispatched.
const MinDispatchConfidence = 0.6

var proposalSchemaVersion = "1"

// ValidateProposal applies the strict versioned schema: exact top-level keys,
// schemaVersion "1", registered or unknown intent enum, confidence in [0,1],
// and per-intent argument bounds. Any violation yields ErrInvalidProposal.
func ValidateProposal(raw json.RawMessage) (domain.IntentProposal, error) {
	invalid := func(reason string) (domain.IntentProposal, error) {
		return domain.IntentProposal{}, fmt.Errorf("%w: %s", domain.ErrInvalidProposal, reason)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return invalid("malformed JSON")
	}
	for _, key := range []string{"schemaVersion", "intent", "arguments", "confidence", "missingFields"} {
		if _, ok := fields[key]; !ok {
			return invalid("missing key " + key)
		}
	}
	if len(fields) != 5 {
		return invalid("unknown top-level key")
	}

	var schemaVersion string
	if err := json.Unmarshal(fields["schemaVersion"], &schemaVersion); err != nil || schemaVersion != proposalSchemaVersion {
		return invalid("schemaVersion must be \"1\"")
	}

	var intent string
	if err := json.Unmarshal(fields["intent"], &intent); err != nil {
		return invalid("intent must be a string")
	}
	switch domain.Intent(intent) {
	case domain.IntentTodoCreate, domain.IntentTodoDelete, domain.IntentTodoList, domain.IntentUnknown:
	default:
		return invalid("unknown intent " + intent)
	}

	var confidence float64
	if err := json.Unmarshal(fields["confidence"], &confidence); err != nil {
		return invalid("confidence must be a number")
	}
	if confidence < 0 || confidence > 1 {
		return invalid("confidence out of range")
	}

	var missingFields []string
	if err := json.Unmarshal(fields["missingFields"], &missingFields); err != nil {
		return invalid("missingFields must be an array of strings")
	}

	var arguments map[string]json.RawMessage
	if err := json.Unmarshal(fields["arguments"], &arguments); err != nil {
		return invalid("arguments must be an object")
	}

	proposal := domain.IntentProposal{
		Intent:        domain.Intent(intent),
		Confidence:    confidence,
		MissingFields: missingFields,
	}
	if proposal.Intent == domain.IntentUnknown {
		return proposal, nil
	}
	if err := validateArguments(&proposal, arguments); err != nil {
		return invalid(err.Error())
	}
	return proposal, nil
}

func validateArguments(proposal *domain.IntentProposal, arguments map[string]json.RawMessage) error {
	decode := func(key string, dst any) error {
		raw, ok := arguments[key]
		if !ok {
			return nil
		}
		return json.Unmarshal(raw, dst)
	}
	requireKeys := func(allowed ...string) error {
		allowedSet := make(map[string]bool, len(allowed))
		for _, key := range allowed {
			allowedSet[key] = true
		}
		for key := range arguments {
			if !allowedSet[key] {
				return fmt.Errorf("unknown argument key %s", key)
			}
		}
		return nil
	}
	parseTime := func(key string) (*time.Time, error) {
		raw, ok := arguments[key]
		if !ok {
			return nil, nil
		}
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("%s must be a string", key)
		}
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return nil, fmt.Errorf("%s must be RFC3339", key)
		}
		return &parsed, nil
	}

	switch proposal.Intent {
	case domain.IntentTodoCreate:
		if err := requireKeys("title", "description", "dueAtUtc", "timezoneAtInput"); err != nil {
			return err
		}
		if raw, ok := arguments["title"]; ok {
			if err := json.Unmarshal(raw, &proposal.Arguments.Title); err != nil {
				return fmt.Errorf("title must be a string")
			}
			if len([]rune(proposal.Arguments.Title)) < 1 || len([]rune(proposal.Arguments.Title)) > maxTitleLength {
				return fmt.Errorf("title must be 1..%d characters", maxTitleLength)
			}
		}
		if raw, ok := arguments["description"]; ok {
			var description string
			if err := json.Unmarshal(raw, &description); err != nil {
				return fmt.Errorf("description must be a string")
			}
			proposal.Arguments.Description = &description
		}
		due, err := parseTime("dueAtUtc")
		if err != nil {
			return err
		}
		proposal.Arguments.DueAtUTC = due
		if raw, ok := arguments["timezoneAtInput"]; ok {
			if err := json.Unmarshal(raw, &proposal.Arguments.Timezone); err != nil {
				return fmt.Errorf("timezoneAtInput must be a string")
			}
			if _, err := time.LoadLocation(proposal.Arguments.Timezone); err != nil {
				return fmt.Errorf("timezoneAtInput must be a valid IANA name")
			}
		}
	case domain.IntentTodoDelete:
		if err := requireKeys("keyword"); err != nil {
			return err
		}
		raw, ok := arguments["keyword"]
		if !ok {
			return fmt.Errorf("keyword is required for todo.delete")
		}
		if err := json.Unmarshal(raw, &proposal.Arguments.Keyword); err != nil {
			return fmt.Errorf("keyword must be a string")
		}
		if len([]rune(proposal.Arguments.Keyword)) < 1 || len([]rune(proposal.Arguments.Keyword)) > MaxKeywordLength {
			return fmt.Errorf("keyword must be 1..%d characters", MaxKeywordLength)
		}
	case domain.IntentTodoList:
		if err := requireKeys("keyword", "status", "dueFrom", "dueTo", "noDue"); err != nil {
			return err
		}
		if err := decode("keyword", &proposal.Arguments.Keyword); err != nil {
			return fmt.Errorf("keyword must be a string")
		}
		if len([]rune(proposal.Arguments.Keyword)) > MaxKeywordLength {
			return fmt.Errorf("keyword must be at most %d characters", MaxKeywordLength)
		}
		if err := decode("status", &proposal.Arguments.Status); err != nil {
			return fmt.Errorf("status must be a string")
		}
		if proposal.Arguments.Status != "" &&
			proposal.Arguments.Status != statusPending &&
			proposal.Arguments.Status != statusCompleted {
			return fmt.Errorf("status must be pending or completed")
		}
		dueFrom, err := parseTime("dueFrom")
		if err != nil {
			return err
		}
		proposal.Arguments.DueFrom = dueFrom
		dueTo, err := parseTime("dueTo")
		if err != nil {
			return err
		}
		proposal.Arguments.DueTo = dueTo
		if err := decode("noDue", &proposal.Arguments.NoDue); err != nil {
			return fmt.Errorf("noDue must be a boolean")
		}
	}
	return nil
}

package application

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

// MaxReplyRunes bounds the model's natural-language reply inside the unified
// turn envelope.
const MaxReplyRunes = 2000

var turnSchemaVersion = "1"

// ValidateModelTurn applies the strict envelope schema: exact top-level keys
// schemaVersion/reply/proposal, schemaVersion "1", a reply of 1..MaxReplyRunes
// visible runes, and a proposal that is either null or passes the untouched v1
// ValidateProposal. Any violation yields domain.ErrInvalidModelTurn; callers
// fail closed to the unsupported kind so an invalid action never executes and
// never masquerades as chat.
func ValidateModelTurn(raw json.RawMessage) (domain.ModelTurn, error) {
	invalid := func(reason string) (domain.ModelTurn, error) {
		return domain.ModelTurn{}, fmt.Errorf("%w: %s", domain.ErrInvalidModelTurn, reason)
	}

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return invalid("malformed JSON")
	}
	for _, key := range []string{"schemaVersion", "reply", "proposal"} {
		if _, ok := fields[key]; !ok {
			return invalid("missing key " + key)
		}
	}
	if len(fields) != 3 {
		return invalid("unknown top-level key")
	}

	var schemaVersion string
	if err := json.Unmarshal(fields["schemaVersion"], &schemaVersion); err != nil || schemaVersion != turnSchemaVersion {
		return invalid("schemaVersion must be \"1\"")
	}

	var reply string
	if err := json.Unmarshal(fields["reply"], &reply); err != nil {
		return invalid("reply must be a string")
	}
	if utf8.RuneCountInString(reply) > MaxReplyRunes {
		return invalid(fmt.Sprintf("reply must be at most %d characters", MaxReplyRunes))
	}
	if strings.TrimSpace(reply) == "" {
		return invalid("reply must not be blank")
	}

	turn := domain.ModelTurn{Reply: reply}
	if string(fields["proposal"]) == "null" {
		return turn, nil
	}
	proposal, err := ValidateProposal(fields["proposal"])
	if err != nil {
		return invalid("proposal failed schema validation")
	}
	turn.Proposal = &proposal
	return turn, nil
}

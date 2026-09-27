package ports

import (
	"context"
	"encoding/json"
)

// HistoryMessage is one stored transcript row handed back to the model as
// multi-turn context. Role is RoleUser or RoleAssistant; Text is already
// truncated by the application. History is ordered oldest first.
type HistoryMessage struct {
	Role string
	Text string
}

// MessageInput is the user turn handed to the model adapter together with
// the recent session history.
type MessageInput struct {
	Text     string
	Timezone string
	History  []HistoryMessage
}

// ModelPort completes one unified turn: a raw JSON envelope carrying both a
// natural-language reply and an optional structured intent proposal. All
// schema validation stays in the application; adapters never validate or
// execute.
type ModelPort interface {
	Complete(ctx context.Context, in MessageInput) (json.RawMessage, error)
}

package ports

import (
	"context"
	"encoding/json"
)

// MessageInput is the user turn handed to the model adapter.
type MessageInput struct {
	Text     string
	Timezone string
}

// ModelPort turns user text into a raw proposal. All schema validation stays
// in the application; adapters never validate or execute.
type ModelPort interface {
	Propose(ctx context.Context, in MessageInput) (json.RawMessage, error)
}

package http

import (
	"context"
	"net/http"
	"time"
)

// DevOutboxMessage is one reminder fake-outbox record exposed by the dev
// outbox. The fields mirror the postgres outbox reader's row so cmd can wire
// it with a thin conversion.
type DevOutboxMessage struct {
	Address   string
	Channel   string
	TodoID    string
	Body      string
	CreatedAt time.Time
}

// DevOutboxStore reads the reminder fake outbox, newest first.
type DevOutboxStore interface {
	LatestByAddress(ctx context.Context, address string, limit int) ([]DevOutboxMessage, error)
}

// NewDevOutboxHandler returns the dev outbox endpoint. It is registered by
// cmd only when double-gated (APP_ENV != production and the enable flag);
// otherwise the route is absent and requests receive the JSON 404 envelope.
// The handler itself performs no gate checks.
func NewDevOutboxHandler(store DevOutboxStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		address := r.URL.Query().Get("address")
		if address == "" {
			writeValidationError(w, r)
			return
		}
		messages, err := store.LatestByAddress(r.Context(), address, 5)
		if err != nil {
			writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
			return
		}
		type messageView struct {
			Address   string    `json:"address"`
			Channel   string    `json:"channel"`
			TodoID    string    `json:"todoId"`
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"createdAt"`
		}
		views := make([]messageView, 0, len(messages))
		for _, message := range messages {
			views = append(views, messageView{
				Address:   message.Address,
				Channel:   message.Channel,
				TodoID:    message.TodoID,
				Body:      message.Body,
				CreatedAt: message.CreatedAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": views})
	}
}

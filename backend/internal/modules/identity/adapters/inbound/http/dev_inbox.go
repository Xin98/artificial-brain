package http

import (
	"context"
	"net/http"
	"time"
)

// DevInboxMessage is a single fake-outbox record exposed by the dev inbox.
type DevInboxMessage struct {
	Address   string
	Channel   string
	Purpose   string
	Code      string
	CreatedAt time.Time
}

// DevInboxStore reads the fake outbox.
type DevInboxStore interface {
	LatestByAddress(ctx context.Context, address string, limit int) ([]DevInboxMessage, error)
}

// NewDevInboxHandler returns the dev inbox endpoint. It is registered only when
// enabled (APP_ENV != production and DEV_INBOX_ENABLED=true); otherwise the route
// is absent and requests receive the JSON 404 envelope.
func NewDevInboxHandler(store DevInboxStore) http.HandlerFunc {
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
			Purpose   string    `json:"purpose"`
			Code      string    `json:"code"`
			CreatedAt time.Time `json:"createdAt"`
		}
		views := make([]messageView, 0, len(messages))
		for _, m := range messages {
			views = append(views, messageView{
				Address:   m.Address,
				Channel:   m.Channel,
				Purpose:   m.Purpose,
				Code:      m.Code,
				CreatedAt: m.CreatedAt,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"messages": views})
	}
}

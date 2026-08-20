// Package http serves the reminder inbound routes: the workspace delivery
// list, the instance-wide ops snapshot, the provider receipt webhook, and the
// double-gated dev outbox. The principal arrives on the context via
// Identity's session middleware; this package never touches cookies or
// tokens.
package http

import (
	"net/http"
	"strconv"
	"time"

	identitydto "github.com/Xin98/artificial-brain/backend/internal/modules/identity/application/dto"
	remindercommand "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/command"
	reminderdto "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/dto"
	reminderquery "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/query"
	reminderdomain "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
)

// Handler serves the session-scoped reminder routes and the provider receipt
// webhook. Parse decodes the signed receipt body; cmd may swap in a
// provider-specific parser (e.g. aliyun.ParseSmsReport), and a nil Parse
// falls back to ParseGenericReceipt. Secret is the shared key the receipt
// webhook's HMAC-SHA256 signature is verified against.
type Handler struct {
	List    *reminderquery.ListDeliveriesHandler
	Ops     *reminderquery.ReminderOpsHandler
	Receipt *remindercommand.RecordReceiptHandler
	Parse   func(body []byte) (reminderdto.ReceiptPayload, error)
	Secret  string
}

// RegisterRoutes registers the reminder routes on mux. The delivery list and
// the ops snapshot are session-scoped and wrapped with the auth middleware;
// the receipt webhook authenticates by HMAC signature instead and carries no
// session. The dev outbox is registered separately by cmd, and only when its
// double gate is open.
func RegisterRoutes(mux *http.ServeMux, auth func(http.Handler) http.Handler, h *Handler) {
	mux.Handle("GET /api/v1/reminders", auth(http.HandlerFunc(h.listDeliveries)))
	mux.Handle("GET /api/v1/ops/reminder", auth(http.HandlerFunc(h.reminderOps)))
	mux.Handle("POST /api/v1/webhooks/receipts/sms", http.HandlerFunc(h.recordReceipt))
}

// List pagination bounds enforced at the edge; the application handler keeps
// its own clamps as defense in depth.
const (
	defaultListLimit = 50
	maxListLimit     = 200
)

// retryingFilter is the accepted status alias for sending∧attempt>0; the
// postgres store maps it, the HTTP layer only validates and passes it
// through.
const retryingFilter = "retrying"

// statusFilterValues are the status values the list endpoint accepts: the
// five lifecycle states plus the retrying alias.
var statusFilterValues = map[string]bool{
	string(reminderdomain.StateScheduled):  true,
	string(reminderdomain.StateSending):    true,
	retryingFilter:                         true,
	string(reminderdomain.StateSucceeded):  true,
	string(reminderdomain.StateFailed):     true,
	string(reminderdomain.StateSuppressed): true,
}

func principalFrom(w http.ResponseWriter, r *http.Request) (identitydto.Principal, bool) {
	principal, ok := identitydto.PrincipalFromContext(r.Context())
	if !ok {
		writeUnauthenticated(w, r)
		return identitydto.Principal{}, false
	}
	return principal, true
}

// deliveryView is the wire shape of one reminder delivery: camelCase with
// unset optionals omitted.
type deliveryView struct {
	ID                string     `json:"id"`
	TodoID            string     `json:"todoId"`
	TodoTitle         string     `json:"todoTitle"`
	Channel           string     `json:"channel"`
	State             string     `json:"state"`
	SuppressionReason *string    `json:"suppressionReason,omitempty"`
	AttemptCount      int        `json:"attemptCount"`
	ProviderMessageID *string    `json:"providerMessageId,omitempty"`
	LastErrorCode     *string    `json:"lastErrorCode,omitempty"`
	ScheduledAt       time.Time  `json:"scheduledAt"`
	CreatedAt         time.Time  `json:"createdAt"`
	SubmittedAt       *time.Time `json:"submittedAt,omitempty"`
	FinalizedAt       *time.Time `json:"finalizedAt,omitempty"`
	ReceiptState      *string    `json:"receiptState,omitempty"`
	ReceiptAt         *time.Time `json:"receiptAt,omitempty"`
	ReceiptErrorCode  *string    `json:"receiptErrorCode,omitempty"`
}

func newDeliveryView(delivery reminderdomain.ReminderDelivery) deliveryView {
	return deliveryView{
		ID:                delivery.ID,
		TodoID:            delivery.TodoID,
		TodoTitle:         delivery.TodoTitleSnapshot,
		Channel:           delivery.Channel,
		State:             string(delivery.State),
		SuppressionReason: suppressionReasonView(delivery.SuppressionReason),
		AttemptCount:      delivery.AttemptCount,
		ProviderMessageID: delivery.ProviderMessageID,
		LastErrorCode:     delivery.LastErrorCode,
		ScheduledAt:       delivery.ScheduledAt,
		CreatedAt:         delivery.CreatedAt,
		SubmittedAt:       delivery.SubmittedAt,
		FinalizedAt:       delivery.FinalizedAt,
		ReceiptState:      receiptStateView(delivery.ReceiptState),
		ReceiptAt:         delivery.ReceiptAt,
		ReceiptErrorCode:  delivery.ReceiptErrorCode,
	}
}

func suppressionReasonView(reason *reminderdomain.SuppressionReason) *string {
	if reason == nil {
		return nil
	}
	value := string(*reason)
	return &value
}

func receiptStateView(state *reminderdomain.ReceiptState) *string {
	if state == nil {
		return nil
	}
	value := string(*state)
	return &value
}

// listDeliveries serves GET /api/v1/reminders: the workspace's reminder
// deliveries with optional status filter and limit/offset pagination.
func (h *Handler) listDeliveries(w http.ResponseWriter, r *http.Request) {
	principal, ok := principalFrom(w, r)
	if !ok {
		return
	}
	query := r.URL.Query()
	status := query.Get("status")
	if status != "" && !statusFilterValues[status] {
		writeValidationError(w, r)
		return
	}
	limit := defaultListLimit
	if raw := query.Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > maxListLimit {
			writeValidationError(w, r)
			return
		}
		limit = parsed
	}
	offset := 0
	if raw := query.Get("offset"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 0 {
			writeValidationError(w, r)
			return
		}
		offset = parsed
	}
	deliveries, err := h.List.Handle(r.Context(), principal.WorkspaceID, reminderdto.DeliveryFilter{
		Status: status,
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error")
		return
	}
	views := make([]deliveryView, 0, len(deliveries))
	for _, delivery := range deliveries {
		views = append(views, newDeliveryView(delivery))
	}
	writeJSON(w, http.StatusOK, map[string]any{"deliveries": views})
}

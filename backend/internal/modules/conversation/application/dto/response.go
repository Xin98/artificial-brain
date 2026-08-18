// Package dto carries the Conversation application's request/response shapes.
package dto

import (
	"time"

	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

// Response kinds returned by the conversation message flow.
const (
	KindTodoCreated          = "todo_created"
	KindClarification        = "clarification"
	KindCandidates           = "candidates"
	KindConfirmationRequired = "confirmation_required"
	KindTodoList             = "todo_list"
	KindTodoDeleted          = "todo_deleted"
	KindNotFound             = "not_found"
	KindUnsupported          = "unsupported"
)

// MessageResponse is the single envelope for all conversation kinds; only
// the fields relevant to the kind are populated.
type MessageResponse struct {
	Kind             string              `json:"kind"`
	Todo             *tododto.Todo       `json:"todo,omitempty"`
	ResolvedDueAtUTC *time.Time          `json:"resolvedDueAtUtc,omitempty"`
	LocalEcho        string              `json:"localEcho,omitempty"`
	TimezoneEcho     string              `json:"timezoneEcho,omitempty"`
	MissingFields    []string            `json:"missingFields,omitempty"`
	Candidates       []tododto.Candidate `json:"candidates,omitempty"`
	ConfirmationID   string              `json:"confirmationId,omitempty"`
	ExpiresAt        *time.Time          `json:"expiresAt,omitempty"`
	Todos            []tododto.Todo      `json:"todos,omitempty"`
	TodoID           string              `json:"todoId,omitempty"`
}

// ConfirmationView is returned when a confirmation request is created.
type ConfirmationView struct {
	ConfirmationID string    `json:"confirmationId"`
	ExpiresAt      time.Time `json:"expiresAt"`
}

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
	// KindChat is the free-conversation reply for turns without a
	// dispatchable intent.
	KindChat = "chat"
)

// MessageResponse is the single envelope for all conversation kinds; only
// the fields relevant to the kind are populated.
type MessageResponse struct {
	Kind string `json:"kind"`
	// Reply carries the model's natural-language answer: always for chat,
	// for clarification when the model supplied one.
	Reply string `json:"reply,omitempty"`
	// SessionID is the session the turn was persisted against; the
	// messages flow always sets it (auto-created sessions included).
	SessionID        string              `json:"sessionId,omitempty"`
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

// SessionView is one session as listed or returned by the session routes.
type SessionView struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SessionListView is the sidebar listing envelope.
type SessionListView struct {
	Sessions []SessionView `json:"sessions"`
}

// MessageView is one persisted transcript row in session history.
type MessageView struct {
	ID             string    `json:"id"`
	Role           string    `json:"role"`
	Body           string    `json:"body"`
	ResolvedIntent *string   `json:"resolvedIntent,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

// SessionHistoryView is the history envelope for one session: the latest
// messages in ascending insertion order.
type SessionHistoryView struct {
	SessionID string        `json:"sessionId"`
	Title     string        `json:"title"`
	Messages  []MessageView `json:"messages"`
}

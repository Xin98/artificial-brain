package domain

import "errors"

var (
	// ErrInvalidProposal rejects model output that fails strict runtime
	// schema validation; an invalid proposal never becomes a write.
	ErrInvalidProposal = errors.New("conversation: proposal failed schema validation")

	ErrConfirmationNotFound          = errors.New("conversation: confirmation request not found")
	ErrConfirmationConsumed          = errors.New("conversation: confirmation request already consumed")
	ErrConfirmationExpired           = errors.New("conversation: confirmation request expired")
	ErrUnsupportedConfirmationIntent = errors.New("conversation: intent cannot be confirmed")
	ErrConfirmationTodoVersionStale  = errors.New("conversation: todo changed since confirmation")

	// ErrTodoNotFound and ErrTodoNotPending are conversation-owned mirrors of
	// Todo's outcomes: the TodoGateway shim translates them so Conversation
	// never imports Todo's domain package.
	ErrTodoNotFound   = errors.New("conversation: todo not found")
	ErrTodoNotPending = errors.New("conversation: todo is not pending")
)

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

	// ErrInvalidModelTurn rejects unified model output whose envelope fails
	// strict validation; like ErrInvalidProposal it never becomes a write and
	// never masquerades as chat.
	ErrInvalidModelTurn = errors.New("conversation: model turn failed envelope validation")

	// ErrInvalidSession marks constructor misuse (missing identifiers);
	// ErrSessionTitleInvalid marks out-of-bounds titles; ErrSessionNotFound
	// is the scoped miss for any session access outside the caller's
	// workspace+user.
	ErrInvalidSession      = errors.New("conversation: session is invalid")
	ErrSessionTitleInvalid = errors.New("conversation: session title is invalid")
	ErrSessionNotFound     = errors.New("conversation: session not found")
)

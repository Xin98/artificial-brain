package domain

import "errors"

// MaxTitleLength bounds todo titles in characters (runes), matching the
// database char_length check.
const MaxTitleLength = 200

var (
	ErrInvalidTitle     = errors.New("todo: title must be between 1 and 200 characters")
	ErrConflict         = errors.New("todo: version conflict")
	ErrTodoNotFound     = errors.New("todo: todo not found")
	ErrTodoDeleted      = errors.New("todo: todo is deleted")
	ErrAlreadyCompleted = errors.New("todo: todo already completed")
	ErrInvalidTimezone  = errors.New("todo: timezone must be a valid IANA name")
)

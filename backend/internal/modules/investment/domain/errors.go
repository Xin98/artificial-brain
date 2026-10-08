package domain

import "errors"

var (
	ErrInvalidInput              = errors.New("invalid_input")
	ErrOverflow                  = errors.New("numeric_overflow")
	ErrNotFound                  = errors.New("not_found")
	ErrDataNotConfigured         = errors.New("data_not_configured")
	ErrDataStale                 = errors.New("data_stale")
	ErrInsufficientUniverse      = errors.New("insufficient_universe")
	ErrFactorUnavailable         = errors.New("factor_unavailable")
	ErrCorporateActionIncomplete = errors.New("corporate_action_incomplete")
	ErrInsufficientCash          = errors.New("insufficient_settled_cash")
	ErrRiskLimit                 = errors.New("risk_limit_exceeded")
	ErrAutomationPaused          = errors.New("automation_paused")
	ErrVersionConflict           = errors.New("version_conflict")
	ErrIdempotencyConflict       = errors.New("idempotency_conflict")
	ErrOrderNotCancellable       = errors.New("order_not_cancellable")
)

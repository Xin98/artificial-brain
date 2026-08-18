package domain

import "errors"

var (
	ErrMissingSchedule    = errors.New("reminder: scheduled time is required")
	ErrPlanAlreadyRevoked = errors.New("reminder: plan already revoked")
	ErrPlanExists         = errors.New("reminder: plan already exists for todo reminder version")
)

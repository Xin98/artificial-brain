package domain

import "errors"

var (
	ErrMissingSchedule    = errors.New("reminder: scheduled time is required")
	ErrPlanAlreadyRevoked = errors.New("reminder: plan already revoked")
	ErrPlanExists         = errors.New("reminder: plan already exists for todo reminder version")

	ErrMissingDeliveryFields  = errors.New("reminder: delivery requires id, workspace, owner, todo, plan, and title")
	ErrInvalidDeliveryChannel = errors.New("reminder: delivery channel must be email or sms")
	ErrDeliveryNotSending     = errors.New("reminder: delivery must be sending to be marked succeeded")
	ErrDeliveryFinal          = errors.New("reminder: delivery is already in a final state")
	ErrReceiptNotApplicable   = errors.New("reminder: receipts only apply to succeeded deliveries")
	ErrDeliveryExists         = errors.New("reminder: delivery already exists")
	ErrDeliveryNotFound       = errors.New("reminder: delivery not found")
)

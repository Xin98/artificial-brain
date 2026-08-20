package domain

import "errors"

var (
	ErrMissingSchedule    = errors.New("reminder: scheduled time is required")
	ErrPlanAlreadyRevoked = errors.New("reminder: plan already revoked")
	ErrPlanExists         = errors.New("reminder: plan already exists for todo reminder version")
	ErrPlanNotFound       = errors.New("reminder: plan not found")

	ErrMissingDeliveryFields  = errors.New("reminder: delivery requires id, workspace, owner, todo, plan, and title")
	ErrInvalidDeliveryChannel = errors.New("reminder: delivery channel must be email or sms")

	// ErrInvalidSuppressionReason rejects a suppression reason that is not one
	// of the five known domain reasons, so callers never persist free-form text.
	ErrInvalidSuppressionReason = errors.New("reminder: suppression reason must be one of the known reasons")
	ErrDeliveryNotSending       = errors.New("reminder: delivery must be sending to be marked succeeded")
	ErrDeliveryFinal            = errors.New("reminder: delivery is already in a final state")
	ErrReceiptNotApplicable     = errors.New("reminder: receipts only apply to succeeded deliveries")
	ErrDeliveryExists           = errors.New("reminder: delivery already exists")
	ErrDeliveryNotFound         = errors.New("reminder: delivery not found")
)

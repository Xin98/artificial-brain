package domain

import "time"

const (
	OrderPending     = "pending"
	OrderAwaitingBar = "awaiting_bar"
	OrderFilled      = "filled"
	OrderPartial     = "partially_filled_cancelled"
	OrderExpired     = "expired"
	OrderRejected    = "rejected"
	OrderCancelled   = "cancelled"
)

type Order struct {
	ID, AccountID, InstrumentID, Side, State, Reason, Origin string
	Quantity, ReservedQuantity, Capacity                     Quantity
	ReservedCash                                             Money
	TargetOpenAt, ExpiresAt, CreatedAt                       time.Time
	Version                                                  int
}

func (o Order) Terminal() bool { return o.State != OrderPending && o.State != OrderAwaitingBar }
func (o Order) Cancellable(now time.Time) bool {
	return o.State == OrderPending && now.Before(o.TargetOpenAt)
}

package dto

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type PlaceOrderRequest struct {
	Mutation
	AccountID    string `json:"-"`
	InstrumentID string `json:"instrumentId"`
	Side         string `json:"side"`
	Quantity     string `json:"quantity"`
}
type CancelOrderRequest struct {
	Mutation
	AccountID, OrderID string `json:"-"`
}
type ExecuteOrdersRequest struct {
	Scope        domain.Scope
	AccountID    string
	SessionDate  time.Time
	FinalAttempt bool
}
type ExecutionResult struct {
	AccountID                 string
	Filled, Expired, Awaiting int
	Blocked                   []domain.Exclusion
}
type FillView struct {
	ID          string     `json:"id"`
	Quantity    string     `json:"quantity"`
	Price       string     `json:"price"`
	Gross       string     `json:"gross"`
	Fee         string     `json:"fee"`
	EffectiveAt time.Time  `json:"effectiveAt"`
	RecordedAt  time.Time  `json:"recordedAt"`
	SettlesAt   *time.Time `json:"settlesAt"`
}
type OrderView struct {
	ID               string    `json:"id"`
	AccountID        string    `json:"accountId"`
	InstrumentID     string    `json:"instrumentId"`
	Side             string    `json:"side"`
	State            string    `json:"state"`
	Reason           string    `json:"reason"`
	Origin           string    `json:"origin"`
	Quantity         string    `json:"quantity"`
	ReservedQuantity string    `json:"reservedQuantity"`
	ReservedCash     string    `json:"reservedCash"`
	Capacity         string    `json:"capacity"`
	Version          int       `json:"version"`
	TargetOpenAt     time.Time `json:"targetOpenAt"`
	ExpiresAt        time.Time `json:"expiresAt"`
	CreatedAt        time.Time `json:"createdAt"`
	Fill             *FillView `json:"fill"`
}

func ViewOrder(o domain.Order) OrderView {
	return OrderView{ID: o.ID, AccountID: o.AccountID, InstrumentID: o.InstrumentID, Side: o.Side, State: o.State, Reason: o.Reason, Origin: o.Origin, Quantity: o.Quantity.String(), ReservedQuantity: o.ReservedQuantity.String(), ReservedCash: o.ReservedCash.String(), Capacity: o.Capacity.String(), Version: o.Version, TargetOpenAt: o.TargetOpenAt, ExpiresAt: o.ExpiresAt, CreatedAt: o.CreatedAt}
}
func ViewFill(f domain.Fill) FillView {
	v := FillView{ID: f.ID, Quantity: f.Quantity.String(), Price: f.Price.String(), Gross: f.Gross.String(), Fee: f.Fee.String(), EffectiveAt: f.EffectiveAt, RecordedAt: f.RecordedAt}
	if !f.SettlesAt.IsZero() {
		v.SettlesAt = &f.SettlesAt
	}
	return v
}

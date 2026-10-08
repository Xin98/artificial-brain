package http

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"net/http"
)

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok {
		return
	}
	var b dto.CreateAccountRequest
	if !decodeJSON(w, r, &b) || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	if b.Mode == "" {
		b.Mode = h.Mode
	}
	if b.StrategyVersionID != "" && !validID(w, r, b.StrategyVersionID) {
		return
	}
	if b.UniverseVersionID != "" && !validID(w, r, b.UniverseVersionID) {
		return
	}
	if b.InitialCash != "" {
		if _, e := domain.ParseMoney(b.InitialCash); e != nil {
			writeError(w, r, e)
			return
		}
	}
	v, e := h.CreateAccount.Handle(r.Context(), b)
	reply(w, r, 201, v, e)
}
func (h *Handler) createUniverse(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok {
		return
	}
	var b dto.CreateUniverseVersionRequest
	if !decodeJSON(w, r, &b, "name", "instrumentIds") || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	if b.Mode == "" {
		b.Mode = h.Mode
	}
	if path := r.PathValue("id"); path != "" {
		if !validID(w, r, path) {
			return
		}
		if b.UniverseID != "" && b.UniverseID != path {
			writeError(w, r, domain.ErrInvalidInput)
			return
		}
		b.UniverseID = path
	}
	v, e := h.CreateUniverse.Handle(r.Context(), b)
	reply(w, r, 201, v, e)
}
func (h *Handler) createStrategy(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok {
		return
	}
	var b dto.CreateStrategyVersionRequest
	if !decodeJSON(w, r, &b, "parameters") || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	path := r.PathValue("id")
	if path != "multifactor-v1" {
		writeError(w, r, domain.ErrNotFound)
		return
	}
	if b.StrategyID != "" && b.StrategyID != path {
		writeError(w, r, domain.ErrInvalidInput)
		return
	}
	b.StrategyID = path
	v, e := h.CreateStrategy.Handle(r.Context(), b)
	reply(w, r, 201, v, e)
}
func (h *Handler) placeOrder(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) {
		return
	}
	var b dto.PlaceOrderRequest
	if !decodeJSON(w, r, &b, "instrumentId", "side", "quantity", "expectedVersion") || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	b.AccountID = r.PathValue("id")
	v, e := h.PlaceOrder.Handle(r.Context(), b)
	reply(w, r, 201, v, e)
}
func (h *Handler) cancelOrder(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) || !validID(w, r, r.PathValue("orderId")) {
		return
	}
	var b dto.CancelOrderRequest
	if !decodeJSON(w, r, &b, "expectedVersion") || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	b.AccountID = r.PathValue("id")
	b.OrderID = r.PathValue("orderId")
	v, e := h.CancelOrder.Handle(r.Context(), b)
	reply(w, r, 200, v, e)
}
func (h *Handler) automation(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) {
		return
	}
	var b dto.ConfigureAutomationRequest
	if !decodeJSON(w, r, &b, "expectedVersion", "enabled", "strategyVersionId", "universeVersionId", "policy") || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	if !validID(w, r, b.StrategyVersionID) || !validID(w, r, b.UniverseVersionID) {
		return
	}
	b.AccountID = r.PathValue("id")
	v, e := h.Automation.Handle(r.Context(), b)
	reply(w, r, 200, v, e)
}
func (h *Handler) evaluate(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok || !validID(w, r, r.PathValue("id")) {
		return
	}
	var b dto.EvaluateRequest
	if !decodeJSON(w, r, &b) || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	b.Scope = scope
	b.AccountID = r.PathValue("id")
	if b.Purpose == "" {
		b.Purpose = "research"
	}
	v, e := h.Evaluate.Start(r.Context(), b)
	reply(w, r, 202, v, e)
}
func (h *Handler) createBacktest(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok {
		return
	}
	var b dto.CreateBacktestRequest
	if !decodeJSON(w, r, &b, "strategyVersionId", "universeVersionId", "from", "to") || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	if !validID(w, r, b.StrategyVersionID) || !validID(w, r, b.UniverseVersionID) {
		return
	}
	v, e := h.CreateBacktest.Handle(r.Context(), b)
	reply(w, r, 202, v, e)
}
func (h *Handler) startSync(w http.ResponseWriter, r *http.Request) {
	scope, ok := scopeFrom(w, r)
	if !ok {
		return
	}
	var b command.StartSyncRequest
	if !decodeJSON(w, r, &b, "from", "to") || !mutation(w, r, scope, &b.Mutation) {
		return
	}
	v, e := h.StartSync.Handle(r.Context(), b)
	reply(w, r, 202, v, e)
}

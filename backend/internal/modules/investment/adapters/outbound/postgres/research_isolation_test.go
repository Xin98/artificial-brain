package postgres

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"testing"
)

func TestResearchEvaluationDoesNotSuppressAutomaticBatch(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	a, err := h.accounts.Get(ctx, h.scope, h.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	a.AutomationEnabled = true
	if err = h.accounts.Save(ctx, a, a.Version); err != nil {
		t.Fatal(err)
	}
	req := dto.EvaluateRequest{Scope: h.scope, AccountID: a.ID, Purpose: "research"}
	research, err := h.evaluator().Handle(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	req.Purpose = "automatic"
	automatic, err := h.evaluator().Handle(ctx, req)
	if err != nil || automatic.ID == research.ID || !automatic.IssuedOrders {
		t.Fatal(research.ID, automatic.ID, automatic.Purpose, automatic.IssuedOrders, err)
	}
}

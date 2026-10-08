package command

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"testing"
)

func TestCreateAccountRejectsInvalidFundingBeforeWork(t *testing.T) {
	h := CreateAccountHandler{}
	for _, s := range []string{"0", "-1", "1e5", "1000000000.01"} {
		_, e := h.Handle(context.Background(), dto.CreateAccountRequest{InitialCash: s})
		if e != domain.ErrInvalidInput {
			t.Fatal(s, e)
		}
	}
}
func TestVersionsImmutableAndPoolLimit(t *testing.T) {
	h := CreateUniverseVersionHandler{}
	_, e := h.Handle(context.Background(), dto.CreateUniverseVersionRequest{InstrumentIDs: make([]string, 101)})
	if e != domain.ErrInvalidInput {
		t.Fatal(e)
	}
	s := CreateStrategyVersionHandler{}
	_, e = s.Handle(context.Background(), dto.CreateStrategyVersionRequest{Parameters: domain.StrategyParameters{}})
	if e != domain.ErrInvalidInput {
		t.Fatal(e)
	}
}
func TestRiskPolicyCannotRelaxHardLimits(t *testing.T) {
	p := domain.DefaultRiskPolicy()
	p.SingleWeight = .2
	if p.Validate() == nil {
		t.Fatal("relaxed limit")
	}
	p = domain.DefaultRiskPolicy()
	p.DrawdownPause = .2
	if p.Validate() == nil {
		t.Fatal("relaxed drawdown")
	}
}

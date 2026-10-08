package fixture

import (
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"reflect"
	"testing"
	"time"
)

func TestFixtureEvaluationReproducible(t *testing.T) {
	a, _ := New()
	asOf, _ := time.Parse(time.RFC3339, "2026-10-06T21:00:00Z")
	s, _ := domain.SelectSnapshot(a.Snapshot, asOf)
	u := domain.UniverseVersion{ID: "u"}
	for _, i := range s.Instruments {
		if i.Kind == "stock" {
			u.InstrumentIDs = append(u.InstrumentIDs, i.ID)
		}
	}
	p := domain.StrategyVersion{ID: "s", Parameters: domain.DefaultStrategyParameters()}
	one, e := domain.Evaluate(s, u, p)
	if e != nil || len(one.Signals) < 10 {
		t.Fatal(one.Excluded, e)
	}
	two, e := domain.Evaluate(s, u, p)
	if e != nil || !reflect.DeepEqual(one, two) {
		t.Fatal("non deterministic")
	}
	for _, x := range one.Signals {
		if x.InstrumentID == "fixture-27" || x.InstrumentID == "fixture-28" || x.InstrumentID == "fixture-29" {
			t.Fatal("ineligible ranked", x)
		}
	}
}

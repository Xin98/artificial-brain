package domain_test

import (
	d "github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"math"
	"testing"
)

func TestPerformanceExactThresholds(t *testing.T) {
	c := []d.NAVPoint{{NAV: 10000}, {NAV: 12000}, {NAV: 9000}}
	p, e := d.ComputePerformance(c, nil, 10000)
	if e != nil || math.Abs(p.CumulativeReturn+.1) > 1e-9 || math.Abs(p.MaxDrawdown-.25) > 1e-9 || p.AnnualReturn != nil || p.Sharpe != nil {
		t.Fatal(p, e)
	}
	constant := make([]d.NAVPoint, 252)
	for n := range constant {
		constant[n].NAV = 10000
	}
	p, e = d.ComputePerformance(constant, nil, 10000)
	if e != nil || p.AnnualReturn == nil || p.Sharpe != nil {
		t.Fatal(p, e)
	}
}
func TestPerformanceSampleBoundaries(t *testing.T) {
	curve := make([]d.NAVPoint, 252)
	for n := range curve {
		curve[n].NAV = 10000 + d.Money(n%2)*100
	}
	p, e := d.ComputePerformance(curve[:59], nil, 10000)
	if e != nil || p.Sharpe != nil {
		t.Fatal(p, e)
	}
	p, e = d.ComputePerformance(curve[:60], nil, 10000)
	if e != nil || p.Sharpe == nil {
		t.Fatal(p, e)
	}
	p, e = d.ComputePerformance(curve[:251], nil, 10000)
	if e != nil || p.AnnualReturn != nil {
		t.Fatal(p, e)
	}
	p, e = d.ComputePerformance(curve, nil, 10000)
	if e != nil || p.AnnualReturn == nil {
		t.Fatal(p, e)
	}
}

package domain

import "testing"

func TestMoneyExactRoundTrip(t *testing.T) {
	for _, s := range []string{"0.00", "100000.00", "92233720368547758.07"} {
		m, e := ParseMoney(s)
		if e != nil || m.String() != s {
			t.Fatalf("%s: %v %v", s, m, e)
		}
	}
}
func TestGrossValueRoundsHalfUp(t *testing.T) {
	for _, c := range []struct {
		p    Price
		q    Quantity
		want Money
	}{{1234567, 100, 12346}, {5000, 1, 1}, {4999, 1, 0}} {
		got, e := GrossValue(c.p, c.q)
		if e != nil || got != c.want {
			t.Fatal(got, e)
		}
	}
}
func TestRejectInvalidDecimalAndOverflow(t *testing.T) {
	for _, s := range []string{"1e5", "NaN", "Infinity", "-1", "1.001", " 1", "92233720368547758.08"} {
		if _, e := ParseMoney(s); e == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"0", "-1", "1.0000001", "1000001"} {
		if _, e := ParsePrice(s); e == nil {
			t.Fatal(s)
		}
	}
	for _, s := range []string{"0", "1.5", "1e3", "1000000001"} {
		if _, e := ParseQuantity(s); e == nil {
			t.Fatal(s)
		}
	}
	if _, e := GrossValue(Price(9223372036854775807), Quantity(1000000000)); e == nil {
		t.Fatal("overflow accepted")
	}
}
func TestDefaultRiskPolicy(t *testing.T) {
	p := DefaultRiskPolicy()
	if p.SingleWeight != .1 || p.IndustryWeight != .3 || p.StockWeight != .8 || p.DrawdownPause != .15 || p.StopLoss != .1 || p.TurnoverLimit != .2 {
		t.Fatal(p)
	}
}
func TestBalanceUnderflowAndOverflow(t *testing.T) {
	if _, e := (Balances{Available: 10}).Apply(Balances{Available: -11}); e == nil {
		t.Fatal("underflow")
	}
	if _, e := (Balances{Available: 9223372036854775807}).Apply(Balances{Available: 1}); e == nil {
		t.Fatal("overflow")
	}
}

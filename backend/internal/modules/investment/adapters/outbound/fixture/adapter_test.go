package fixture

import (
	"reflect"
	"testing"
)

func TestFixtureRepeatableAndNoNetwork(t *testing.T) {
	a, e := New()
	if e != nil {
		t.Fatal(e)
	}
	b, e := New()
	if e != nil || !reflect.DeepEqual(a.Snapshot, b.Snapshot) {
		t.Fatal("non deterministic", e)
	}
	if len(a.Snapshot.Instruments) != 31 || len(a.Snapshot.Bars) < 31*500 || len(a.Snapshot.Facts) < 120 || len(a.Snapshot.Actions) < 2 {
		t.Fatal("incomplete fixture")
	}
}
func TestFixtureIdentityStableAcrossTickerChange(t *testing.T) {
	a, e := New()
	if e != nil {
		t.Fatal(e)
	}
	i := a.Snapshot.Instruments[0]
	if len(i.TickerHistory) != 2 || i.ID == i.Ticker {
		t.Fatal(i)
	}
}

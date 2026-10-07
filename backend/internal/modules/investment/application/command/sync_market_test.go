package command

import (
	"testing"
)

func TestMarketSyncModeVersionIsolation(t *testing.T) {
	a := DatasetVersion("fixture", "synthetic", "v1")
	b := DatasetVersion("alpaca_sec", "iex", "v1")
	c := DatasetVersion("alpaca_sec", "sip", "v1")
	if a == b || b == c || a == c {
		t.Fatal(a, b, c)
	}
}

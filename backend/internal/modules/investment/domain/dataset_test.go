package domain

import "testing"

func TestAccountCannotStitchDatasetVersions(t *testing.T) {
	a := Account{Mode: "alpaca_sec", DatasetVersion: "alpaca_sec/iex/v1"}
	if e := ValidateAccountDataset(a, Snapshot{Mode: "alpaca_sec", DatasetVersion: "alpaca_sec/sip/v1"}); e != ErrVersionConflict {
		t.Fatal(e)
	}
	if e := ValidateAccountDataset(a, Snapshot{Mode: "fixture", DatasetVersion: a.DatasetVersion}); e != ErrVersionConflict {
		t.Fatal(e)
	}
}

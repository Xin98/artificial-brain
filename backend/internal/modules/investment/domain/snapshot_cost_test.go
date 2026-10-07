package domain

import (
	"fmt"
	"testing"
	"time"
)

// Replays repeatedly select the same fact history. Comparison-time formatting
// caused the complete race suite to exceed its existing ten-minute deadline.
func TestSnapshotSelectionAllocationBudget(t *testing.T) {
	at := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	input := Snapshot{AsOf: at}
	for i := 0; i < 400; i++ {
		input.Facts = append(input.Facts, FinancialFact{InstrumentID: "test", Concept: fmt.Sprintf("concept-%04d", 399-i), PeriodStart: at.AddDate(-1, 0, 0), PeriodEnd: at.AddDate(0, -1, 0), Unit: "USD", Currency: "USD", AvailableAt: at, Value: "1"})
	}
	var selected Snapshot
	allocations := testing.AllocsPerRun(2, func() {
		var err error
		selected, err = SelectSnapshot(input, at)
		if err != nil {
			panic(err)
		}
	})
	if len(selected.Facts) != 400 {
		t.Fatal("facts lost", len(selected.Facts))
	}
	if allocations > 10000 {
		t.Fatalf("selection allocated %.0f objects for 400 facts; budget 10000", allocations)
	}
}

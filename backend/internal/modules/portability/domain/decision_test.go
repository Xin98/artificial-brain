package domain

import (
	"reflect"
	"testing"
)

func TestDecideUnseenEntryIsNew(t *testing.T) {
	entries := []ImportEntry{{Kind: KindTodo, SourceRecordID: "todo-1", Fingerprint: "fp-1"}}
	decisions := Decide(entries, map[string]string{})
	if len(decisions) != 1 {
		t.Fatalf("Decide() returned %d decisions, want 1", len(decisions))
	}
	want := Decision{Kind: KindTodo, SourceRecordID: "todo-1", Outcome: OutcomeNew}
	if decisions[0] != want {
		t.Fatalf("Decide() = %#v, want %#v", decisions[0], want)
	}
}

func TestDecideNilExistingMakesEverythingNew(t *testing.T) {
	entries := []ImportEntry{
		{Kind: KindTodo, SourceRecordID: "todo-1", Fingerprint: "fp-1"},
		{Kind: KindChannel, SourceRecordID: "channel-1", Fingerprint: "fp-2"},
	}
	decisions := Decide(entries, nil)
	for _, decision := range decisions {
		if decision.Outcome != OutcomeNew {
			t.Fatalf("Decide(nil existing) outcome = %q for %q, want %q", decision.Outcome, decision.SourceRecordID, OutcomeNew)
		}
	}
}

func TestDecideSameFingerprintIsSkipped(t *testing.T) {
	entries := []ImportEntry{{Kind: KindTodo, SourceRecordID: "todo-1", Fingerprint: "fp-1"}}
	existing := map[string]string{"instance-1:todo-1": "fp-1"}
	decisions := Decide(entries, existing)
	if decisions[0].Outcome != OutcomeSkipped {
		t.Fatalf("Decide(equal fingerprint) outcome = %q, want %q", decisions[0].Outcome, OutcomeSkipped)
	}
	if decisions[0].Reason == "" {
		t.Fatalf("Decide(skipped) reason is empty, want a reason")
	}
}

func TestDecideChangedFingerprintIsConflict(t *testing.T) {
	entries := []ImportEntry{{Kind: KindTodo, SourceRecordID: "todo-1", Fingerprint: "fp-2"}}
	existing := map[string]string{"instance-1:todo-1": "fp-1"}
	decisions := Decide(entries, existing)
	if decisions[0].Outcome != OutcomeConflict {
		t.Fatalf("Decide(different fingerprint) outcome = %q, want %q", decisions[0].Outcome, OutcomeConflict)
	}
	if decisions[0].Reason == "" {
		t.Fatalf("Decide(conflict) reason is empty, want a reason")
	}
}

func TestDecideKeepsInputOrderForMixedEntries(t *testing.T) {
	entries := []ImportEntry{
		{Kind: KindTodo, SourceRecordID: "todo-new", Fingerprint: "fp-a"},
		{Kind: KindTodo, SourceRecordID: "todo-same", Fingerprint: "fp-b"},
		{Kind: KindDelivery, SourceRecordID: "delivery-changed", Fingerprint: "fp-c2"},
		{Kind: KindChannel, SourceRecordID: "channel-new", Fingerprint: "fp-d"},
	}
	existing := map[string]string{
		"instance-1:todo-same":        "fp-b",
		"instance-1:delivery-changed": "fp-c1",
	}
	decisions := Decide(entries, existing)
	gotOutcomes := make([]Outcome, len(decisions))
	gotIDs := make([]string, len(decisions))
	for index, decision := range decisions {
		gotOutcomes[index] = decision.Outcome
		gotIDs[index] = decision.SourceRecordID
	}
	wantOutcomes := []Outcome{OutcomeNew, OutcomeSkipped, OutcomeConflict, OutcomeNew}
	if !reflect.DeepEqual(gotOutcomes, wantOutcomes) {
		t.Fatalf("Decide() outcomes = %v, want %v", gotOutcomes, wantOutcomes)
	}
	wantIDs := []string{"todo-new", "todo-same", "delivery-changed", "channel-new"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("Decide() record ids = %v, want %v", gotIDs, wantIDs)
	}
}

func TestDecideIgnoresOtherRecordIDs(t *testing.T) {
	entries := []ImportEntry{{Kind: KindTodo, SourceRecordID: "todo-1", Fingerprint: "fp-1"}}
	existing := map[string]string{
		"instance-1:todo-2":    "fp-1", // same fingerprint, different record id
		"instance-1:todo-10":   "fp-1", // record id that merely prefixes ours must not match
		"todo-1":               "fp-1", // malformed key without instance prefix must not match
		"other-instance:todoX": "fp-1",
	}
	decisions := Decide(entries, existing)
	if decisions[0].Outcome != OutcomeNew {
		t.Fatalf("Decide(unrelated keys) outcome = %q, want %q", decisions[0].Outcome, OutcomeNew)
	}
}

func TestDecideEqualFingerprintWinsAcrossInstances(t *testing.T) {
	entries := []ImportEntry{{Kind: KindTodo, SourceRecordID: "todo-1", Fingerprint: "fp-1"}}
	existing := map[string]string{
		"instance-1:todo-1": "fp-old",
		"instance-2:todo-1": "fp-1",
	}
	decisions := Decide(entries, existing)
	if decisions[0].Outcome != OutcomeSkipped {
		t.Fatalf("Decide(one equal fingerprint among several) outcome = %q, want %q", decisions[0].Outcome, OutcomeSkipped)
	}
}

func TestDecideEmptyEntries(t *testing.T) {
	if decisions := Decide(nil, map[string]string{"instance-1:todo-1": "fp-1"}); len(decisions) != 0 {
		t.Fatalf("Decide(nil entries) returned %d decisions, want 0", len(decisions))
	}
}

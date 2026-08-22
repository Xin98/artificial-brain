package domain

import (
	"encoding/hex"
	"testing"
	"time"
)

func assertSHA256Hex(t *testing.T, value string) {
	t.Helper()
	if len(value) != 64 {
		t.Fatalf("fingerprint %q has length %d, want 64 hex chars", value, len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		t.Fatalf("fingerprint %q is not valid hex: %v", value, err)
	}
}

func TestFingerprintStableAcrossFieldOrder(t *testing.T) {
	first := map[string]any{}
	first["id"] = "todo-1"
	first["title"] = "写周报"
	first["status"] = "pending"

	second := map[string]any{}
	second["status"] = "pending"
	second["title"] = "写周报"
	second["id"] = "todo-1"

	if Fingerprint(first) != Fingerprint(second) {
		t.Fatalf("Fingerprint differs for the same record built in another field order: %q vs %q", Fingerprint(first), Fingerprint(second))
	}
	assertSHA256Hex(t, Fingerprint(first))
}

func TestFingerprintChangesWhenAnyFieldChanges(t *testing.T) {
	base := map[string]any{"id": "todo-1", "title": "写周报", "status": "pending", "count": 1}
	baseFingerprint := Fingerprint(base)

	mutations := map[string]map[string]any{
		"id changed":     {"id": "todo-2", "title": "写周报", "status": "pending", "count": 1},
		"title changed":  {"id": "todo-1", "title": "写月报", "status": "pending", "count": 1},
		"status changed": {"id": "todo-1", "title": "写周报", "status": "completed", "count": 1},
		"count changed":  {"id": "todo-1", "title": "写周报", "status": "pending", "count": 2},
		"field added":    {"id": "todo-1", "title": "写周报", "status": "pending", "count": 1, "extra": true},
	}
	for name, mutated := range mutations {
		if Fingerprint(mutated) == baseFingerprint {
			t.Fatalf("Fingerprint unchanged for mutation %q, want a different digest", name)
		}
	}
}

func TestFingerprintPointerNilVsSetDiffer(t *testing.T) {
	description := "补充说明"
	due := time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC)

	withNil := TodoRecord{ID: "todo-1", Title: "写周报", Status: TodoStatusPending, CreatedAt: testExportedAt, UpdatedAt: testExportedAt}
	withSet := withNil
	withSet.Description = &description
	if Fingerprint(withNil) == Fingerprint(withSet) {
		t.Fatalf("Fingerprint identical for nil vs set Description, want different digests")
	}

	withNilDue := withNil
	withDue := withNil
	withDue.DueAtUTC = &due
	if Fingerprint(withNilDue) == Fingerprint(withDue) {
		t.Fatalf("Fingerprint identical for nil vs set DueAtUTC, want different digests")
	}
}

func TestFingerprintStructMatchesEquivalentMap(t *testing.T) {
	record := TodoRecord{ID: "todo-1", Title: "写周报", Status: TodoStatusPending, CreatedAt: testExportedAt, UpdatedAt: testExportedAt}
	equivalent := map[string]any{
		"ID":              "todo-1",
		"Title":           "写周报",
		"Description":     nil,
		"DueAtUTC":        nil,
		"TimezoneAtInput": nil,
		"Status":          TodoStatusPending,
		"ReminderVersion": 0,
		"CreatedAt":       testExportedAt,
		"UpdatedAt":       testExportedAt,
		"CompletedAt":     nil,
		"DeletedAt":       nil,
	}
	if Fingerprint(record) != Fingerprint(equivalent) {
		t.Fatalf("Fingerprint(struct) = %q, Fingerprint(map) = %q, want equal canonical digests", Fingerprint(record), Fingerprint(equivalent))
	}
}

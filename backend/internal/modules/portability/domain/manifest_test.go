package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var testExportedAt = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)

func validManifest() Manifest {
	return Manifest{
		SchemaVersion:    SchemaVersion,
		SourceInstanceID: "instance-1",
		ExportedAt:       testExportedAt,
		Counts:           ManifestCounts{Todos: 2, Deliveries: 1, Channels: 1},
		Files: map[string]string{
			"todos.json":               strings.Repeat("a", 64),
			"reminder-deliveries.json": strings.Repeat("b", 64),
			"preferences.json":         strings.Repeat("c", 64),
			"todos.csv":                strings.Repeat("d", 64),
		},
	}
}

func TestValidateManifestAcceptsValidManifest(t *testing.T) {
	if err := ValidateManifest(validManifest()); err != nil {
		t.Fatalf("ValidateManifest(valid) error = %v, want nil", err)
	}
}

func TestValidateManifestRejectsUnsupportedSchemaVersion(t *testing.T) {
	manifest := validManifest()
	manifest.SchemaVersion = "2"
	if err := ValidateManifest(manifest); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("ValidateManifest(version 2) error = %v, want ErrUnsupportedSchemaVersion", err)
	}
	// Schema version is checked before any other field.
	manifest.SourceInstanceID = ""
	if err := ValidateManifest(manifest); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("ValidateManifest(version 2 + empty id) error = %v, want ErrUnsupportedSchemaVersion", err)
	}
	manifest = validManifest()
	manifest.SchemaVersion = ""
	if err := ValidateManifest(manifest); !errors.Is(err, ErrUnsupportedSchemaVersion) {
		t.Fatalf("ValidateManifest(empty version) error = %v, want ErrUnsupportedSchemaVersion", err)
	}
}

func TestValidateManifestRejectsInvalidFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
	}{
		{"empty source instance id", func(m *Manifest) { m.SourceInstanceID = "" }},
		{"zero exported at", func(m *Manifest) { m.ExportedAt = time.Time{} }},
		{"negative todos count", func(m *Manifest) { m.Counts.Todos = -1 }},
		{"negative deliveries count", func(m *Manifest) { m.Counts.Deliveries = -2 }},
		{"negative channels count", func(m *Manifest) { m.Counts.Channels = -3 }},
		{"nil files", func(m *Manifest) { m.Files = nil }},
		{"empty files", func(m *Manifest) { m.Files = map[string]string{} }},
		{"empty file name", func(m *Manifest) { m.Files[""] = strings.Repeat("e", 64) }},
		{"empty checksum", func(m *Manifest) { m.Files["todos.json"] = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manifest := validManifest()
			test.mutate(&manifest)
			err := ValidateManifest(manifest)
			if !errors.Is(err, ErrManifestInvalid) {
				t.Fatalf("ValidateManifest(%s) error = %v, want ErrManifestInvalid", test.name, err)
			}
		})
	}
}

func validTodoRecord() TodoRecord {
	return TodoRecord{
		ID:              "todo-1",
		Title:           "写周报",
		Status:          TodoStatusPending,
		ReminderVersion: 1,
		CreatedAt:       testExportedAt,
		UpdatedAt:       testExportedAt,
	}
}

func TestValidateTodoRecord(t *testing.T) {
	if err := ValidateTodoRecord(validTodoRecord()); err != nil {
		t.Fatalf("ValidateTodoRecord(valid) error = %v, want nil", err)
	}
	for _, status := range []string{TodoStatusPending, TodoStatusCompleted, TodoStatusDeleted} {
		record := validTodoRecord()
		record.Status = status
		if err := ValidateTodoRecord(record); err != nil {
			t.Fatalf("ValidateTodoRecord(status %q) error = %v, want nil", status, err)
		}
	}

	tests := []struct {
		name   string
		mutate func(*TodoRecord)
	}{
		{"empty id", func(r *TodoRecord) { r.ID = "" }},
		{"empty title", func(r *TodoRecord) { r.Title = "" }},
		{"unknown status", func(r *TodoRecord) { r.Status = "archived" }},
		{"empty status", func(r *TodoRecord) { r.Status = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := validTodoRecord()
			test.mutate(&record)
			if err := ValidateTodoRecord(record); !errors.Is(err, ErrRecordInvalid) {
				t.Fatalf("ValidateTodoRecord(%s) error = %v, want ErrRecordInvalid", test.name, err)
			}
		})
	}
}

func TestValidateTodoRecordNamesOffendingRecord(t *testing.T) {
	record := validTodoRecord()
	record.Title = ""
	err := ValidateTodoRecord(record)
	if err == nil || !strings.Contains(err.Error(), record.ID) {
		t.Fatalf("ValidateTodoRecord(empty title) error = %v, want it to name record %q", err, record.ID)
	}
}

func validChannelRecord() ChannelRecord {
	return ChannelRecord{ID: "channel-1", Kind: ChannelKindEmail, Address: "ops@example.com", Enabled: true}
}

func TestValidateChannelRecord(t *testing.T) {
	if err := ValidateChannelRecord(validChannelRecord()); err != nil {
		t.Fatalf("ValidateChannelRecord(valid) error = %v, want nil", err)
	}
	for _, kind := range []string{ChannelKindEmail, ChannelKindSMS} {
		record := validChannelRecord()
		record.Kind = kind
		if err := ValidateChannelRecord(record); err != nil {
			t.Fatalf("ValidateChannelRecord(kind %q) error = %v, want nil", kind, err)
		}
	}

	tests := []struct {
		name   string
		mutate func(*ChannelRecord)
	}{
		{"empty id", func(r *ChannelRecord) { r.ID = "" }},
		{"unknown kind", func(r *ChannelRecord) { r.Kind = "wechat" }},
		{"empty kind", func(r *ChannelRecord) { r.Kind = "" }},
		{"empty address", func(r *ChannelRecord) { r.Address = "" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := validChannelRecord()
			test.mutate(&record)
			if err := ValidateChannelRecord(record); !errors.Is(err, ErrRecordInvalid) {
				t.Fatalf("ValidateChannelRecord(%s) error = %v, want ErrRecordInvalid", test.name, err)
			}
		})
	}
}

func validDeliveryRecord() DeliveryRecord {
	return DeliveryRecord{
		ID:                 "delivery-1",
		SourceTodoRecordID: "todo-1",
		Channel:            ChannelKindEmail,
		State:              DeliveryStateSucceeded,
		AttemptCount:       1,
		TodoTitleSnapshot:  "写周报",
		ScheduledAt:        testExportedAt,
		CreatedAt:          testExportedAt,
		Origin:             DeliveryOriginLocal,
	}
}

func TestValidateDeliveryRecord(t *testing.T) {
	if err := ValidateDeliveryRecord(validDeliveryRecord()); err != nil {
		t.Fatalf("ValidateDeliveryRecord(valid) error = %v, want nil", err)
	}
	for _, state := range []string{
		DeliveryStateScheduled, DeliveryStateSending, DeliveryStateSucceeded,
		DeliveryStateFailed, DeliveryStateSuppressed,
	} {
		record := validDeliveryRecord()
		record.State = state
		if err := ValidateDeliveryRecord(record); err != nil {
			t.Fatalf("ValidateDeliveryRecord(state %q) error = %v, want nil", state, err)
		}
	}
	for _, origin := range []string{DeliveryOriginLocal, DeliveryOriginImported} {
		record := validDeliveryRecord()
		record.Origin = origin
		if err := ValidateDeliveryRecord(record); err != nil {
			t.Fatalf("ValidateDeliveryRecord(origin %q) error = %v, want nil", origin, err)
		}
	}

	tests := []struct {
		name   string
		mutate func(*DeliveryRecord)
	}{
		{"empty id", func(r *DeliveryRecord) { r.ID = "" }},
		{"empty source todo record id", func(r *DeliveryRecord) { r.SourceTodoRecordID = "" }},
		{"unknown channel", func(r *DeliveryRecord) { r.Channel = "wechat" }},
		{"empty channel", func(r *DeliveryRecord) { r.Channel = "" }},
		{"unknown state", func(r *DeliveryRecord) { r.State = "queued" }},
		{"empty state", func(r *DeliveryRecord) { r.State = "" }},
		{"negative attempt count", func(r *DeliveryRecord) { r.AttemptCount = -1 }},
		{"unknown origin", func(r *DeliveryRecord) { r.Origin = "elsewhere" }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			record := validDeliveryRecord()
			test.mutate(&record)
			if err := ValidateDeliveryRecord(record); !errors.Is(err, ErrRecordInvalid) {
				t.Fatalf("ValidateDeliveryRecord(%s) error = %v, want ErrRecordInvalid", test.name, err)
			}
		})
	}
}

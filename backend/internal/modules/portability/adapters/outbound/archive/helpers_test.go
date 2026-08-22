package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

var (
	bundleExportedAt = time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	todoCreatedAt    = time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	todoDueAt        = time.Date(2026, 8, 2, 9, 0, 0, 0, time.UTC)
	deliveryAt       = time.Date(2026, 8, 2, 8, 0, 0, 0, time.UTC)
)

func strPtr(value string) *string        { return &value }
func timePtr(value time.Time) *time.Time { return &value }

// sampleTodos covers both optional shapes: a pending todo carrying every
// optional field, and a completed todo with only completedAt set.
func sampleTodos() []dto.TodoExportRecord {
	return []dto.TodoExportRecord{
		{
			ID:              "todo-1",
			Title:           "buy milk",
			Description:     strPtr("oat milk"),
			DueAtUTC:        timePtr(todoDueAt),
			TimezoneAtInput: strPtr("Asia/Shanghai"),
			Status:          domain.TodoStatusPending,
			ReminderVersion: 1,
			CreatedAt:       todoCreatedAt,
			UpdatedAt:       todoCreatedAt.Add(time.Hour),
		},
		{
			ID:          "todo-2",
			Title:       "file taxes",
			Status:      domain.TodoStatusCompleted,
			CreatedAt:   todoCreatedAt,
			UpdatedAt:   todoCreatedAt.Add(2 * time.Hour),
			CompletedAt: timePtr(todoCreatedAt.Add(2 * time.Hour)),
		},
	}
}

// sampleDeliveries covers a succeeded delivery with provider metadata and a
// suppressed delivery carrying a suppression reason.
func sampleDeliveries() []dto.DeliveryExportRecord {
	return []dto.DeliveryExportRecord{
		{
			ID:                 "delivery-1",
			SourceTodoRecordID: "todo-1",
			Channel:            domain.ChannelKindEmail,
			State:              domain.DeliveryStateSucceeded,
			AttemptCount:       1,
			ProviderMessageID:  strPtr("provider-message-1"),
			TodoTitleSnapshot:  "buy milk",
			ScheduledAt:        deliveryAt,
			CreatedAt:          deliveryAt,
			SubmittedAt:        timePtr(deliveryAt.Add(time.Minute)),
			FinalizedAt:        timePtr(deliveryAt.Add(time.Minute)),
			Origin:             domain.DeliveryOriginLocal,
		},
		{
			ID:                 "delivery-2",
			SourceTodoRecordID: "todo-2",
			Channel:            domain.ChannelKindSMS,
			State:              domain.DeliveryStateSuppressed,
			SuppressionReason:  strPtr(domain.SuppressionReasonChannelUnavailable),
			AttemptCount:       0,
			TodoTitleSnapshot:  "file taxes",
			ScheduledAt:        deliveryAt,
			CreatedAt:          deliveryAt,
			FinalizedAt:        timePtr(deliveryAt.Add(time.Minute)),
			Origin:             domain.DeliveryOriginImported,
		},
	}
}

func sampleChannels() []dto.ChannelExportRecord {
	return []dto.ChannelExportRecord{
		{ID: "channel-1", Kind: domain.ChannelKindEmail, Address: "user@example.com", Enabled: true},
		{ID: "channel-2", Kind: domain.ChannelKindSMS, Address: "+8613800137001", Enabled: false},
	}
}

// sampleCSV is a realistic todos.csv body; the parser checksums it but never
// reads its rows.
const sampleCSV = "id,title,status,dueAtUtc,timezoneAtInput,createdAt,completedAt,deletedAt\n" +
	"todo-1,buy milk,pending,2026-08-02T09:00:00Z,Asia/Shanghai,2026-08-01T09:00:00Z,,\n" +
	"todo-2,file taxes,completed,,,2026-08-01T09:00:00Z,2026-08-01T11:00:00Z,\n"

// bundleSpec describes a bundle to build through the production Writer.
type bundleSpec struct {
	todos         []dto.TodoExportRecord
	deliveries    []dto.DeliveryExportRecord
	channels      []dto.ChannelExportRecord
	csv           []byte
	schemaVersion string // defaults to domain.SchemaVersion
	instanceID    string // defaults to "instance-1"
	omitEntry     string // one contract entry name to skip entirely
	extraName     string // one unexpected entry appended before the manifest
	extraContent  []byte
}

// buildBundle streams the spec through NewWriter and returns the bundle bytes
// plus the manifest as finalized — its shared Files map is filled by
// WriteManifest, mirroring what the export handler observes.
func buildBundle(t *testing.T, spec bundleSpec) ([]byte, domain.Manifest) {
	t.Helper()
	var buf bytes.Buffer
	writer := NewWriter(&buf)
	ctx := context.Background()

	writeJSON := func(name string, records any) {
		t.Helper()
		if name == spec.omitEntry {
			return
		}
		data, err := json.Marshal(records)
		if err != nil {
			t.Fatalf("buildBundle: marshal %q: %v", name, err)
		}
		err = writer.WriteEntry(ctx, name, func(_ context.Context, w io.Writer) error {
			_, err := w.Write(data)
			return err
		})
		if err != nil {
			t.Fatalf("buildBundle: write %q: %v", name, err)
		}
	}

	writeJSON(dto.TodosEntry, orEmptyTodos(spec.todos))
	writeJSON(dto.DeliveriesEntry, orEmptyDeliveries(spec.deliveries))
	writeJSON(dto.PreferencesEntry, orEmptyChannels(spec.channels))

	if dto.TodosCSVEntry != spec.omitEntry {
		csvData := spec.csv
		if err := writer.WriteEntry(ctx, dto.TodosCSVEntry, func(_ context.Context, w io.Writer) error {
			_, err := w.Write(csvData)
			return err
		}); err != nil {
			t.Fatalf("buildBundle: write %q: %v", dto.TodosCSVEntry, err)
		}
	}

	if spec.extraName != "" {
		extra := spec.extraContent
		if err := writer.WriteEntry(ctx, spec.extraName, func(_ context.Context, w io.Writer) error {
			_, err := w.Write(extra)
			return err
		}); err != nil {
			t.Fatalf("buildBundle: write extra %q: %v", spec.extraName, err)
		}
	}

	schemaVersion := spec.schemaVersion
	if schemaVersion == "" {
		schemaVersion = domain.SchemaVersion
	}
	instanceID := spec.instanceID
	if instanceID == "" {
		instanceID = "instance-1"
	}
	manifest := domain.Manifest{
		SchemaVersion:    schemaVersion,
		SourceInstanceID: instanceID,
		ExportedAt:       bundleExportedAt,
		Counts: domain.ManifestCounts{
			Todos:      len(spec.todos),
			Deliveries: len(spec.deliveries),
			Channels:   len(spec.channels),
		},
		Files: map[string]string{},
	}
	if err := writer.WriteManifest(ctx, manifest); err != nil {
		t.Fatalf("buildBundle: WriteManifest: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("buildBundle: Close: %v", err)
	}
	return buf.Bytes(), manifest
}

// json.Marshal renders a nil slice as null; the exporter always writes [] so
// the builder keeps that shape for empty record sets.
func orEmptyTodos(records []dto.TodoExportRecord) []dto.TodoExportRecord {
	if records == nil {
		return []dto.TodoExportRecord{}
	}
	return records
}

func orEmptyDeliveries(records []dto.DeliveryExportRecord) []dto.DeliveryExportRecord {
	if records == nil {
		return []dto.DeliveryExportRecord{}
	}
	return records
}

func orEmptyChannels(records []dto.ChannelExportRecord) []dto.ChannelExportRecord {
	if records == nil {
		return []dto.ChannelExportRecord{}
	}
	return records
}

// readZipFile reads one zip entry's full contents.
func readZipFile(t *testing.T, file *zip.File) []byte {
	t.Helper()
	reader, err := file.Open()
	if err != nil {
		t.Fatalf("open zip entry %q: %v", file.Name, err)
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read zip entry %q: %v", file.Name, err)
	}
	return data
}

// rewriteZip rebuilds the zip entry-by-entry, applying modify to each entry;
// modify returns the entry's new content and whether to keep the entry. The
// manifest entry passes through untouched so only data entries change.
func rewriteZip(t *testing.T, data []byte, modify func(name string, content []byte) ([]byte, bool)) []byte {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("rewriteZip: open source: %v", err)
	}
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for _, file := range reader.File {
		content := readZipFile(t, file)
		newContent, keep := modify(file.Name, content)
		if !keep {
			continue
		}
		entry, err := writer.Create(file.Name)
		if err != nil {
			t.Fatalf("rewriteZip: create %q: %v", file.Name, err)
		}
		if _, err := entry.Write(newContent); err != nil {
			t.Fatalf("rewriteZip: write %q: %v", file.Name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("rewriteZip: close: %v", err)
	}
	return buf.Bytes()
}

package archive

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// wantTodoDomains is the hand-written domain mapping of sampleTodos.
func wantTodoDomains() []domain.TodoRecord {
	return []domain.TodoRecord{
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

// wantDeliveryDomains is the hand-written domain mapping of sampleDeliveries.
func wantDeliveryDomains() []domain.DeliveryRecord {
	return []domain.DeliveryRecord{
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

// wantChannelDomains is the hand-written domain mapping of sampleChannels.
func wantChannelDomains() []domain.ChannelRecord {
	return []domain.ChannelRecord{
		{ID: "channel-1", Kind: domain.ChannelKindEmail, Address: "user@example.com", Enabled: true},
		{ID: "channel-2", Kind: domain.ChannelKindSMS, Address: "+8613800137001", Enabled: false},
	}
}

func fullSpec() bundleSpec {
	return bundleSpec{
		todos:      sampleTodos(),
		deliveries: sampleDeliveries(),
		channels:   sampleChannels(),
		csv:        []byte(sampleCSV),
	}
}

// TestParseRoundTripsWriterBundle pins the full round trip: a bundle streamed
// by the production Writer parses back to equal records and manifest.
func TestParseRoundTripsWriterBundle(t *testing.T) {
	data, manifest := buildBundle(t, fullSpec())

	bundle, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if !reflect.DeepEqual(bundle.Manifest, manifest) {
		t.Fatalf("Parse() manifest = %+v, want %+v", bundle.Manifest, manifest)
	}
	if !reflect.DeepEqual(bundle.Todos, wantTodoDomains()) {
		t.Fatalf("Parse() todos = %#v, want %#v", bundle.Todos, wantTodoDomains())
	}
	if !reflect.DeepEqual(bundle.Deliveries, wantDeliveryDomains()) {
		t.Fatalf("Parse() deliveries = %#v, want %#v", bundle.Deliveries, wantDeliveryDomains())
	}
	if !reflect.DeepEqual(bundle.Channels, wantChannelDomains()) {
		t.Fatalf("Parse() channels = %#v, want %#v", bundle.Channels, wantChannelDomains())
	}
}

// TestParseEmptyArraysRoundTrip pins that an empty workspace's bundle parses
// back to empty record sets and zero counts.
func TestParseEmptyArraysRoundTrip(t *testing.T) {
	data, _ := buildBundle(t, bundleSpec{csv: []byte("id,title,status,dueAtUtc,timezoneAtInput,createdAt,completedAt,deletedAt\n")})

	bundle, err := Parse(data)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if len(bundle.Todos) != 0 || len(bundle.Deliveries) != 0 || len(bundle.Channels) != 0 {
		t.Fatalf("Parse() records = %d todos, %d deliveries, %d channels, want all empty",
			len(bundle.Todos), len(bundle.Deliveries), len(bundle.Channels))
	}
	if bundle.Manifest.Counts != (domain.ManifestCounts{}) {
		t.Fatalf("Parse() counts = %+v, want zero counts", bundle.Manifest.Counts)
	}
}

func TestParseMissingEntryReportsBundleStructure(t *testing.T) {
	for _, name := range dataEntryOrder() {
		t.Run(name, func(t *testing.T) {
			spec := fullSpec()
			spec.omitEntry = name
			data, _ := buildBundle(t, spec)

			_, err := Parse(data)
			if !errors.Is(err, domain.ErrBundleStructure) {
				t.Fatalf("Parse() error = %v, want ErrBundleStructure", err)
			}
			if !strings.Contains(err.Error(), name) {
				t.Fatalf("Parse() error = %v, want the missing entry named", err)
			}
		})
	}
}

func TestParseUnexpectedEntryReportsBundleStructure(t *testing.T) {
	spec := fullSpec()
	spec.extraName = "evil.txt"
	spec.extraContent = []byte("malicious payload")
	data, _ := buildBundle(t, spec)

	_, err := Parse(data)
	if !errors.Is(err, domain.ErrBundleStructure) {
		t.Fatalf("Parse() error = %v, want ErrBundleStructure", err)
	}
	if !strings.Contains(err.Error(), "evil.txt") {
		t.Fatalf("Parse() error = %v, want the unexpected entry named", err)
	}
}

func TestParseUnsupportedSchemaVersion(t *testing.T) {
	spec := fullSpec()
	spec.schemaVersion = "2"
	data, _ := buildBundle(t, spec)

	_, err := Parse(data)
	if !errors.Is(err, domain.ErrUnsupportedSchemaVersion) {
		t.Fatalf("Parse() error = %v, want ErrUnsupportedSchemaVersion", err)
	}
}

// TestParseCorruptedEntryReportsChecksumMismatch flips one byte of todos.json
// after the manifest is finalized, so the structure and manifest stay valid
// while the checksum no longer matches.
func TestParseCorruptedEntryReportsChecksumMismatch(t *testing.T) {
	data, _ := buildBundle(t, fullSpec())
	corrupted := rewriteZip(t, data, func(name string, content []byte) ([]byte, bool) {
		if name != dto.TodosEntry {
			return content, true
		}
		content[0] ^= 0xFF
		return content, true
	})

	_, err := Parse(corrupted)
	if !errors.Is(err, domain.ErrChecksumMismatch) {
		t.Fatalf("Parse() error = %v, want ErrChecksumMismatch", err)
	}
	if !strings.Contains(err.Error(), dto.TodosEntry) {
		t.Fatalf("Parse() error = %v, want the corrupted entry named", err)
	}
}

func TestParseInvalidRecordReportsRecordInvalid(t *testing.T) {
	t.Run("todo with empty title names the record", func(t *testing.T) {
		spec := fullSpec()
		spec.todos = []dto.TodoExportRecord{{
			ID:        "todo-bad",
			Title:     "",
			Status:    domain.TodoStatusPending,
			CreatedAt: todoCreatedAt,
			UpdatedAt: todoCreatedAt,
		}}
		data, _ := buildBundle(t, spec)

		_, err := Parse(data)
		if !errors.Is(err, domain.ErrRecordInvalid) {
			t.Fatalf("Parse() error = %v, want ErrRecordInvalid", err)
		}
		if !strings.Contains(err.Error(), "todo-bad") {
			t.Fatalf("Parse() error = %v, want the record id named", err)
		}
	})

	t.Run("todo with unknown status", func(t *testing.T) {
		spec := fullSpec()
		spec.todos = []dto.TodoExportRecord{{
			ID:        "todo-bad",
			Title:     "bad status",
			Status:    "archived",
			CreatedAt: todoCreatedAt,
			UpdatedAt: todoCreatedAt,
		}}
		data, _ := buildBundle(t, spec)

		_, err := Parse(data)
		if !errors.Is(err, domain.ErrRecordInvalid) {
			t.Fatalf("Parse() error = %v, want ErrRecordInvalid", err)
		}
	})

	t.Run("channel with unknown kind", func(t *testing.T) {
		spec := fullSpec()
		spec.channels = []dto.ChannelExportRecord{{ID: "channel-bad", Kind: "pigeon", Address: "rooftop"}}
		data, _ := buildBundle(t, spec)

		_, err := Parse(data)
		if !errors.Is(err, domain.ErrRecordInvalid) {
			t.Fatalf("Parse() error = %v, want ErrRecordInvalid", err)
		}
		if !strings.Contains(err.Error(), "channel-bad") {
			t.Fatalf("Parse() error = %v, want the record id named", err)
		}
	})

	t.Run("todo with title over the todo domain limit", func(t *testing.T) {
		spec := fullSpec()
		spec.todos = []dto.TodoExportRecord{{
			ID:        "todo-bad",
			Title:     strings.Repeat("x", domain.MaxTodoTitleLength+1),
			Status:    domain.TodoStatusPending,
			CreatedAt: todoCreatedAt,
			UpdatedAt: todoCreatedAt,
		}}
		data, _ := buildBundle(t, spec)

		_, err := Parse(data)
		if !errors.Is(err, domain.ErrRecordInvalid) {
			t.Fatalf("Parse() error = %v, want ErrRecordInvalid", err)
		}
	})

	t.Run("suppressed delivery without a suppression reason", func(t *testing.T) {
		spec := fullSpec()
		spec.deliveries = []dto.DeliveryExportRecord{{
			ID:                 "delivery-bad",
			SourceTodoRecordID: "todo-1",
			Channel:            domain.ChannelKindEmail,
			State:              domain.DeliveryStateSuppressed,
			ScheduledAt:        deliveryAt,
			CreatedAt:          deliveryAt,
			Origin:             domain.DeliveryOriginLocal,
		}}
		data, _ := buildBundle(t, spec)

		_, err := Parse(data)
		if !errors.Is(err, domain.ErrRecordInvalid) {
			t.Fatalf("Parse() error = %v, want ErrRecordInvalid", err)
		}
	})

	t.Run("delivery with unknown state", func(t *testing.T) {
		spec := fullSpec()
		spec.deliveries = []dto.DeliveryExportRecord{{
			ID:                 "delivery-bad",
			SourceTodoRecordID: "todo-1",
			Channel:            domain.ChannelKindEmail,
			State:              "exploded",
			ScheduledAt:        deliveryAt,
			CreatedAt:          deliveryAt,
			Origin:             domain.DeliveryOriginLocal,
		}}
		data, _ := buildBundle(t, spec)

		_, err := Parse(data)
		if !errors.Is(err, domain.ErrRecordInvalid) {
			t.Fatalf("Parse() error = %v, want ErrRecordInvalid", err)
		}
		if !strings.Contains(err.Error(), "delivery-bad") {
			t.Fatalf("Parse() error = %v, want the record id named", err)
		}
	})
}

// zeroReader streams an unbounded run of zero bytes; it lets a test craft a
// highly compressible oversized entry without allocating it.
type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

// TestParseRejectsOversizedEntry guards the decompression bomb edge: the
// bundle cap bounds compressed bytes, so a zero-filled entry can claim a
// decompressed size orders of magnitude larger. The parser must cap each
// entry's decompressed size and report ErrBundleStructure instead of
// allocating the claimed payload.
func TestParseRejectsOversizedEntry(t *testing.T) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create(dto.TodosEntry)
	if err != nil {
		t.Fatalf("zip.Create() error = %v", err)
	}
	if _, err := io.CopyN(entry, zeroReader{}, maxEntryBytes+1); err != nil {
		t.Fatalf("write oversized entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v", err)
	}

	_, err = Parse(buf.Bytes())
	if !errors.Is(err, domain.ErrBundleStructure) {
		t.Fatalf("Parse() error = %v, want ErrBundleStructure", err)
	}
	if !strings.Contains(err.Error(), dto.TodosEntry) {
		t.Fatalf("Parse() error = %v, want the oversized entry named", err)
	}
}

// TestParseAcceptsEntryAtSizeLimit pins that the cap is inclusive: an entry
// of exactly maxEntryBytes decompressed bytes reads fine (the failure above
// comes from the structure violation, which is what the test asserts, so this
// case drives readEntry directly to keep the boundary honest).
func TestParseReadEntryAtSizeLimit(t *testing.T) {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	entry, err := writer.Create("bounded.bin")
	if err != nil {
		t.Fatalf("zip.Create() error = %v", err)
	}
	if _, err := io.CopyN(entry, zeroReader{}, maxEntryBytes); err != nil {
		t.Fatalf("write limit-size entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("zip.Close() error = %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	content, err := readEntry(reader.File[0])
	if err != nil {
		t.Fatalf("readEntry(at limit) error = %v, want nil", err)
	}
	if len(content) != maxEntryBytes {
		t.Fatalf("readEntry(at limit) len = %d, want %d", len(content), maxEntryBytes)
	}
}

func TestParseNonZipReportsBundleStructure(t *testing.T) {
	for name, data := range map[string][]byte{
		"garbage": []byte("this is not a zip archive"),
		"empty":   {},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(data)
			if !errors.Is(err, domain.ErrBundleStructure) {
				t.Fatalf("Parse() error = %v, want ErrBundleStructure", err)
			}
		})
	}
}

// TestParserSatisfiesBundleParser pins that Parser implements the T9 port the
// upload handler consumes.
func TestParserSatisfiesBundleParser(t *testing.T) {
	var parser ports.BundleParser = NewParser()
	data, manifest := buildBundle(t, fullSpec())

	bundle, err := parser.Parse(data)
	if err != nil {
		t.Fatalf("Parser.Parse() error = %v", err)
	}
	if !reflect.DeepEqual(bundle.Manifest, manifest) {
		t.Fatalf("Parser.Parse() manifest = %+v, want %+v", bundle.Manifest, manifest)
	}
	if len(bundle.Todos) != len(sampleTodos()) {
		t.Fatalf("Parser.Parse() todos = %d, want %d", len(bundle.Todos), len(sampleTodos()))
	}
}

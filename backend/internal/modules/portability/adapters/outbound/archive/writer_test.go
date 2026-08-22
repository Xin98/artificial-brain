package archive

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

var errEncodeFailed = errors.New("encode failed")

func dataEntryOrder() []string {
	return []string{dto.TodosEntry, dto.DeliveriesEntry, dto.PreferencesEntry, dto.TodosCSVEntry}
}

// TestWriterStreamsEntriesInOrderWithCorrectHashes builds a bundle with four
// data entries plus the manifest, reopens it with archive/zip, and asserts
// the entry order, byte-equal contents, and one correct sha256 per entry.
func TestWriterStreamsEntriesInOrderWithCorrectHashes(t *testing.T) {
	contents := map[string]string{
		dto.TodosEntry:       `[{"id":"todo-1"}]`,
		dto.DeliveriesEntry:  `[{"id":"delivery-1"}]`,
		dto.PreferencesEntry: `[{"id":"channel-1"}]`,
		dto.TodosCSVEntry:    sampleCSV,
	}

	var buf bytes.Buffer
	writer := NewWriter(&buf)
	ctx := context.Background()
	for _, name := range dataEntryOrder() {
		content := contents[name]
		err := writer.WriteEntry(ctx, name, func(_ context.Context, w io.Writer) error {
			_, err := io.WriteString(w, content)
			return err
		})
		if err != nil {
			t.Fatalf("WriteEntry(%q) error = %v", name, err)
		}
	}
	manifest := domain.Manifest{
		SchemaVersion:    domain.SchemaVersion,
		SourceInstanceID: "instance-1",
		ExportedAt:       bundleExportedAt,
		Counts:           domain.ManifestCounts{Todos: 1, Deliveries: 1, Channels: 1},
		Files:            map[string]string{},
	}
	if err := writer.WriteManifest(ctx, manifest); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	reader, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("Close() did not finalize a valid zip: %v", err)
	}

	var names []string
	for _, file := range reader.File {
		names = append(names, file.Name)
	}
	if want := append(dataEntryOrder(), dto.ManifestEntry); !reflect.DeepEqual(names, want) {
		t.Fatalf("entry order = %v, want %v", names, want)
	}

	for _, file := range reader.File {
		data := readZipFile(t, file)
		if file.Name == dto.ManifestEntry {
			continue
		}
		if string(data) != contents[file.Name] {
			t.Fatalf("entry %q content = %q, want byte-equal streamed bytes", file.Name, data)
		}
		sum := sha256.Sum256(data)
		if manifest.Files[file.Name] != hex.EncodeToString(sum[:]) {
			t.Fatalf("recorded hash for %q = %q, want sha256 of streamed bytes", file.Name, manifest.Files[file.Name])
		}
	}
	if _, ok := manifest.Files[dto.ManifestEntry]; ok {
		t.Fatalf("manifest files list itself: %v", manifest.Files)
	}
}

// TestWriteManifestFillsSharedMapBeforeMarshal pins that the marshaled
// manifest.json entry already carries the per-entry hashes — the fill happens
// BEFORE marshaling — and that manifest.json is appended last.
func TestWriteManifestFillsSharedMapBeforeMarshal(t *testing.T) {
	data, manifest := buildBundle(t, bundleSpec{
		todos:      sampleTodos(),
		deliveries: sampleDeliveries(),
		channels:   sampleChannels(),
		csv:        []byte(sampleCSV),
	})

	if len(manifest.Files) != len(dataEntryOrder()) {
		t.Fatalf("shared Files map = %v, want one hash per data entry", manifest.Files)
	}

	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("bundle is not a valid zip: %v", err)
	}
	last := reader.File[len(reader.File)-1]
	if last.Name != dto.ManifestEntry {
		t.Fatalf("last entry = %q, want %q", last.Name, dto.ManifestEntry)
	}

	var wire dto.BundleManifest
	if err := json.Unmarshal(readZipFile(t, last), &wire); err != nil {
		t.Fatalf("manifest.json does not parse: %v", err)
	}
	if !reflect.DeepEqual(wire.Files, manifest.Files) {
		t.Fatalf("marshaled manifest files = %v, want the filled shared map %v", wire.Files, manifest.Files)
	}
	for _, name := range dataEntryOrder() {
		if wire.Files[name] == "" {
			t.Fatalf("marshaled manifest missing hash for %q", name)
		}
	}
	if wire.SchemaVersion != domain.SchemaVersion || wire.SourceInstanceID != "instance-1" {
		t.Fatalf("marshaled manifest = %+v, want schema %q and instance-1", wire, domain.SchemaVersion)
	}
}

// TestWriteManifestRequiresNonNilFiles pins the shared-map contract: the
// handler always passes a non-nil Files map, and the writer refuses to fill a
// map the caller could not observe.
func TestWriteManifestRequiresNonNilFiles(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter(&buf)
	err := writer.WriteManifest(context.Background(), domain.Manifest{
		SchemaVersion:    domain.SchemaVersion,
		SourceInstanceID: "instance-1",
		ExportedAt:       bundleExportedAt,
		Files:            nil,
	})
	if err == nil {
		t.Fatalf("WriteManifest() with nil Files map must fail")
	}
}

// TestWriteEntryEncodeErrorSurfaces pins that an encode failure aborts the
// entry write and is not masked by the archive.
func TestWriteEntryEncodeErrorSurfaces(t *testing.T) {
	var buf bytes.Buffer
	writer := NewWriter(&buf)
	err := writer.WriteEntry(context.Background(), dto.TodosEntry, func(context.Context, io.Writer) error {
		return errEncodeFailed
	})
	if !errors.Is(err, errEncodeFailed) {
		t.Fatalf("WriteEntry() error = %v, want errEncodeFailed", err)
	}
}

// TestFactorySatisfiesArchiveFactory pins that Factory builds writers usable
// through the ports.ArchiveWriter contract the export handler consumes.
func TestFactorySatisfiesArchiveFactory(t *testing.T) {
	var factory ports.ArchiveFactory = Factory()
	var buf bytes.Buffer
	writer := factory(&buf)
	if writer == nil {
		t.Fatalf("Factory() returned a nil writer")
	}
	err := writer.WriteEntry(context.Background(), dto.TodosEntry, func(_ context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "[]")
		return err
	})
	if err != nil {
		t.Fatalf("WriteEntry() error = %v", err)
	}
	if err := writer.WriteManifest(context.Background(), domain.Manifest{
		SchemaVersion:    domain.SchemaVersion,
		SourceInstanceID: "instance-1",
		ExportedAt:       time.Now().UTC(),
		Files:            map[string]string{},
	}); err != nil {
		t.Fatalf("WriteManifest() error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len())); err != nil {
		t.Fatalf("factory writer did not finalize a valid zip: %v", err)
	}
}

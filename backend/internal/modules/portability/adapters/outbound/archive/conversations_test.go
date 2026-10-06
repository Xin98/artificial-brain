package archive

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

func TestConversationBundleVersionTwoRequiresHistoryEntries(t *testing.T) {
	data, _ := buildBundle(t, bundleSpec{schemaVersion: "2"})
	_, err := Parse(data)
	if !errors.Is(err, domain.ErrBundleStructure) {
		t.Fatalf("missing conversation entries = %v, want structure error", err)
	}
}

func TestVersionOneBundleRemainsReadable(t *testing.T) {
	data, _ := buildBundle(t, bundleSpec{schemaVersion: "1", todos: sampleTodos()})
	parsed, err := Parse(data)
	if err != nil || len(parsed.Todos) != 2 {
		t.Fatalf("legacy bundle = %#v, %v", parsed, err)
	}
}

func TestVersionOneWriterRetainsClosedManifestCounts(t *testing.T) {
	data, _ := buildBundle(t, bundleSpec{schemaVersion: "1"})
	entries, err := readEntries(data)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if err := json.Unmarshal(entries["manifest.json"], &wire); err != nil {
		t.Fatal(err)
	}
	counts := wire["counts"].(map[string]any)
	if len(counts) != 3 {
		t.Fatalf("schema1 counts gained unsupported fields: %#v", counts)
	}
}

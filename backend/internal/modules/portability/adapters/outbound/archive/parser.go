package archive

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// requiredEntries is the exact entry set of a valid bundle, in contract
// order; the manifest is always the final entry and is never listed in its
// own files map.
var requiredEntries = []string{
	dto.TodosEntry,
	dto.DeliveriesEntry,
	dto.PreferencesEntry,
	dto.TodosCSVEntry,
	dto.ManifestEntry,
}

// maxEntryBytes caps one entry's decompressed size. The upload cap bounds
// compressed bytes only, so a zero-filled zip can claim a decompressed size
// orders of magnitude larger than its wire size; reading past this bound
// reports ErrBundleStructure instead of allocating the claimed payload.
const maxEntryBytes = 64 << 20

// checksummedEntries are the entries whose bytes must match the manifest's
// recorded sha256. todos.csv is checksummed but never parsed: its rows are a
// human-readable copy, and records come from the JSON entries.
var checksummedEntries = []string{
	dto.TodosEntry,
	dto.DeliveriesEntry,
	dto.PreferencesEntry,
	dto.TodosCSVEntry,
}

// Parse validates and decodes an export bundle's bytes, in order: zip
// structure and the exact entry set, the manifest's schema version and
// fields, every data entry's sha256 against the manifest, and finally each
// record against the domain validators. Errors report the typed domain
// sentinels — ErrBundleStructure, ErrUnsupportedSchemaVersion,
// ErrManifestInvalid, ErrChecksumMismatch, ErrRecordInvalid — so callers
// never store or execute a bundle that failed validation.
func Parse(data []byte) (ports.ParsedBundle, error) {
	entries, err := readEntries(data)
	if err != nil {
		return ports.ParsedBundle{}, err
	}
	manifest, err := parseManifest(entries[dto.ManifestEntry])
	if err != nil {
		return ports.ParsedBundle{}, err
	}
	if err := verifyChecksums(entries, manifest); err != nil {
		return ports.ParsedBundle{}, err
	}
	return decodeRecords(entries, manifest)
}

// Parser implements ports.BundleParser over Parse.
type Parser struct{}

// NewParser returns a Parser.
func NewParser() *Parser { return &Parser{} }

var _ ports.BundleParser = (*Parser)(nil)

// Parse validates and decodes the bundle bytes; see Parse.
func (p *Parser) Parse(data []byte) (ports.ParsedBundle, error) {
	return Parse(data)
}

// readEntries opens the zip and returns every entry's bytes keyed by name,
// reporting ErrBundleStructure for unreadable archives, duplicate entries,
// missing entries, and unexpected entries.
func readEntries(data []byte) (map[string][]byte, error) {
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", domain.ErrBundleStructure, err)
	}
	entries := make(map[string][]byte, len(reader.File))
	for _, file := range reader.File {
		if _, dup := entries[file.Name]; dup {
			return nil, fmt.Errorf("%w: duplicate entry %q", domain.ErrBundleStructure, file.Name)
		}
		content, err := readEntry(file)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", domain.ErrBundleStructure, err)
		}
		entries[file.Name] = content
	}
	if err := checkEntrySet(entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// checkEntrySet requires exactly the contract's entry names: every required
// entry present and no entry beyond them.
func checkEntrySet(entries map[string][]byte) error {
	for _, name := range requiredEntries {
		if _, ok := entries[name]; !ok {
			return fmt.Errorf("%w: missing entry %q", domain.ErrBundleStructure, name)
		}
	}
	for name := range entries {
		if !isRequiredEntry(name) {
			return fmt.Errorf("%w: unexpected entry %q", domain.ErrBundleStructure, name)
		}
	}
	return nil
}

func isRequiredEntry(name string) bool {
	for _, entry := range requiredEntries {
		if entry == name {
			return true
		}
	}
	return false
}

// readEntry decompresses one entry, capped at maxEntryBytes+1 bytes so an
// oversized entry bounds its allocation before it is rejected — the zip wire
// size is compressed bytes only, so a crafted entry can claim a decompressed
// size orders of magnitude larger than the whole bundle.
func readEntry(file *zip.File) ([]byte, error) {
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	content, err := io.ReadAll(io.LimitReader(reader, maxEntryBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %q: %s", file.Name, err)
	}
	if int64(len(content)) > maxEntryBytes {
		return nil, fmt.Errorf("entry %q exceeds the %d-byte decompressed limit", file.Name, maxEntryBytes)
	}
	return content, nil
}

// parseManifest decodes the manifest wire shape and runs the domain
// validation: ValidateManifest checks the schema version first — reporting
// ErrUnsupportedSchemaVersion — before trusting any other field.
func parseManifest(data []byte) (domain.Manifest, error) {
	var wire dto.BundleManifest
	if err := json.Unmarshal(data, &wire); err != nil {
		return domain.Manifest{}, fmt.Errorf("%w: manifest.json does not parse: %s", domain.ErrManifestInvalid, err)
	}
	manifest := domain.Manifest{
		SchemaVersion:    wire.SchemaVersion,
		SourceInstanceID: wire.SourceInstanceID,
		ExportedAt:       wire.ExportedAt,
		Counts: domain.ManifestCounts{
			Todos:      wire.Counts.Todos,
			Deliveries: wire.Counts.Deliveries,
			Channels:   wire.Counts.Channels,
		},
		Files: wire.Files,
	}
	if err := domain.ValidateManifest(manifest); err != nil {
		return domain.Manifest{}, err
	}
	return manifest, nil
}

// verifyChecksums recomputes each data entry's sha256 and compares it with
// the manifest's recorded digest; a missing or mismatched digest reports
// ErrChecksumMismatch naming the entry.
func verifyChecksums(entries map[string][]byte, manifest domain.Manifest) error {
	for _, name := range checksummedEntries {
		want, ok := manifest.Files[name]
		if !ok {
			return fmt.Errorf("%w: manifest carries no checksum for %q", domain.ErrChecksumMismatch, name)
		}
		sum := sha256.Sum256(entries[name])
		if got := hex.EncodeToString(sum[:]); got != want {
			return fmt.Errorf("%w: %q", domain.ErrChecksumMismatch, name)
		}
	}
	return nil
}

// decodeRecords decodes each JSON entry through its bundle wire shape — the
// dto records carry the camelCase JSON tags the exporter marshals — maps the
// records into their domain shapes, and validates them; the first offender
// reports the domain validator's ErrRecordInvalid, which names the record.
func decodeRecords(entries map[string][]byte, manifest domain.Manifest) (ports.ParsedBundle, error) {
	bundle := ports.ParsedBundle{
		Manifest:   manifest,
		Todos:      []domain.TodoRecord{},
		Deliveries: []domain.DeliveryRecord{},
		Channels:   []domain.ChannelRecord{},
	}

	var todoWires []dto.TodoExportRecord
	if err := json.Unmarshal(entries[dto.TodosEntry], &todoWires); err != nil {
		return ports.ParsedBundle{}, fmt.Errorf("%w: %s does not parse: %s", domain.ErrRecordInvalid, dto.TodosEntry, err)
	}
	for _, wire := range todoWires {
		record := todoFromWire(wire)
		if err := domain.ValidateTodoRecord(record); err != nil {
			return ports.ParsedBundle{}, err
		}
		bundle.Todos = append(bundle.Todos, record)
	}

	var deliveryWires []dto.DeliveryExportRecord
	if err := json.Unmarshal(entries[dto.DeliveriesEntry], &deliveryWires); err != nil {
		return ports.ParsedBundle{}, fmt.Errorf("%w: %s does not parse: %s", domain.ErrRecordInvalid, dto.DeliveriesEntry, err)
	}
	for _, wire := range deliveryWires {
		record := deliveryFromWire(wire)
		if err := domain.ValidateDeliveryRecord(record); err != nil {
			return ports.ParsedBundle{}, err
		}
		bundle.Deliveries = append(bundle.Deliveries, record)
	}

	var channelWires []dto.ChannelExportRecord
	if err := json.Unmarshal(entries[dto.PreferencesEntry], &channelWires); err != nil {
		return ports.ParsedBundle{}, fmt.Errorf("%w: %s does not parse: %s", domain.ErrRecordInvalid, dto.PreferencesEntry, err)
	}
	for _, wire := range channelWires {
		record := channelFromWire(wire)
		if err := domain.ValidateChannelRecord(record); err != nil {
			return ports.ParsedBundle{}, err
		}
		bundle.Channels = append(bundle.Channels, record)
	}

	return bundle, nil
}

func todoFromWire(wire dto.TodoExportRecord) domain.TodoRecord {
	return domain.TodoRecord{
		ID:              wire.ID,
		Title:           wire.Title,
		Description:     wire.Description,
		DueAtUTC:        wire.DueAtUTC,
		TimezoneAtInput: wire.TimezoneAtInput,
		Status:          wire.Status,
		ReminderVersion: wire.ReminderVersion,
		CreatedAt:       wire.CreatedAt,
		UpdatedAt:       wire.UpdatedAt,
		CompletedAt:     wire.CompletedAt,
		DeletedAt:       wire.DeletedAt,
	}
}

func deliveryFromWire(wire dto.DeliveryExportRecord) domain.DeliveryRecord {
	return domain.DeliveryRecord{
		ID:                 wire.ID,
		SourceTodoRecordID: wire.SourceTodoRecordID,
		Channel:            wire.Channel,
		State:              wire.State,
		SuppressionReason:  wire.SuppressionReason,
		ProviderMessageID:  wire.ProviderMessageID,
		LastErrorCode:      wire.LastErrorCode,
		AttemptCount:       wire.AttemptCount,
		TodoTitleSnapshot:  wire.TodoTitleSnapshot,
		ScheduledAt:        wire.ScheduledAt,
		CreatedAt:          wire.CreatedAt,
		SubmittedAt:        wire.SubmittedAt,
		FinalizedAt:        wire.FinalizedAt,
		ReceiptState:       wire.ReceiptState,
		ReceiptErrorCode:   wire.ReceiptErrorCode,
		ReceiptAt:          wire.ReceiptAt,
		Origin:             wire.Origin,
	}
}

func channelFromWire(wire dto.ChannelExportRecord) domain.ChannelRecord {
	return domain.ChannelRecord{
		ID:      wire.ID,
		Kind:    wire.Kind,
		Address: wire.Address,
		Enabled: wire.Enabled,
	}
}

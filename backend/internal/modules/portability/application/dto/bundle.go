// Package dto holds the Portability application's wire shapes: the bundle
// record shapes shared with the archive adapter (JSON tags match
// contracts/export-schemas exactly) and the preview/report shapes reported by
// the import flow.
package dto

import (
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// Bundle entry names, fixed by the export bundle contract. The manifest is
// always the final entry and is never listed in its own files map.
const (
	TodosEntry       = "todos.json"
	DeliveriesEntry  = "reminder-deliveries.json"
	PreferencesEntry = "preferences.json"
	TodosCSVEntry    = "todos.csv"
	ManifestEntry    = "manifest.json"
)

// TodoExportRecord is one todo row in the bundle wire shape: full history —
// pending, completed, and deleted — with historical timestamps and versions.
type TodoExportRecord struct {
	ID              string     `json:"id"`
	Title           string     `json:"title"`
	Description     *string    `json:"description,omitempty"`
	DueAtUTC        *time.Time `json:"dueAtUtc,omitempty"`
	TimezoneAtInput *string    `json:"timezoneAtInput,omitempty"`
	Status          string     `json:"status"`
	ReminderVersion int        `json:"reminderVersion"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
	DeletedAt       *time.Time `json:"deletedAt,omitempty"`
}

// ChannelExportRecord is one contact channel row in the bundle wire shape:
// identity, kind, address, and preference only — never verification state or
// code hashes.
type ChannelExportRecord struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Address string `json:"address"`
	Enabled bool   `json:"enabled"`
}

// DeliveryExportRecord is one reminder delivery row in the bundle wire shape.
// SourceTodoRecordID carries the todo id the reminder module stored, which is
// the todo's source record id; import resolves it to the restored todo id.
type DeliveryExportRecord struct {
	ID                 string     `json:"id"`
	SourceTodoRecordID string     `json:"sourceTodoRecordId"`
	Channel            string     `json:"channel"`
	State              string     `json:"state"`
	SuppressionReason  *string    `json:"suppressionReason,omitempty"`
	AttemptCount       int        `json:"attemptCount"`
	ProviderMessageID  *string    `json:"providerMessageId,omitempty"`
	LastErrorCode      *string    `json:"lastErrorCode,omitempty"`
	TodoTitleSnapshot  string     `json:"todoTitleSnapshot"`
	ScheduledAt        time.Time  `json:"scheduledAt"`
	CreatedAt          time.Time  `json:"createdAt"`
	SubmittedAt        *time.Time `json:"submittedAt,omitempty"`
	FinalizedAt        *time.Time `json:"finalizedAt,omitempty"`
	ReceiptState       *string    `json:"receiptState,omitempty"`
	ReceiptErrorCode   *string    `json:"receiptErrorCode,omitempty"`
	ReceiptAt          *time.Time `json:"receiptAt,omitempty"`
	Origin             string     `json:"origin"`
}

// BundleManifest is the wire shape of the bundle's manifest.json entry.
type BundleManifest struct {
	SchemaVersion    string               `json:"schemaVersion"`
	SourceInstanceID string               `json:"sourceInstanceId"`
	ExportedAt       time.Time            `json:"exportedAt"`
	Counts           BundleManifestCounts `json:"counts"`
	Files            map[string]string    `json:"files"`
}

// BundleManifestCounts reports how many records of each kind the bundle
// carries.
type BundleManifestCounts struct {
	Todos      int `json:"todos"`
	Deliveries int `json:"deliveries"`
	Channels   int `json:"channels"`
}

// NewBundleManifest maps the domain manifest to its bundle wire shape.
func NewBundleManifest(manifest domain.Manifest) BundleManifest {
	return BundleManifest{
		SchemaVersion:    manifest.SchemaVersion,
		SourceInstanceID: manifest.SourceInstanceID,
		ExportedAt:       manifest.ExportedAt,
		Counts: BundleManifestCounts{
			Todos:      manifest.Counts.Todos,
			Deliveries: manifest.Counts.Deliveries,
			Channels:   manifest.Counts.Channels,
		},
		Files: manifest.Files,
	}
}

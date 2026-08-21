package dto

import "time"

// Import row states persisted on portability_imports. Expired is rendered
// lazily by reads — a pending row past its TTL — and is never written back.
const (
	ImportStatePending   = "pending"
	ImportStateCommitted = "committed"
	ImportStateExpired   = "expired"
)

// ImportRecordRow is one portability_imports row as seen by the application
// layer: the uploaded bundle bytes stored verbatim, plus the lifecycle fields
// the upload/confirm/get flows need.
//
// Decision (T9): the preview computed at upload time is stored on the row
// (Preview) so GetImportQuery can return it without re-parsing the bundle;
// ConfirmImportHandler never reads it — confirm always re-parses the stored
// bytes and re-decides from scratch, so the stored preview can never make an
// execution decision stale.
type ImportRecordRow struct {
	ID               string
	WorkspaceID      string
	State            string
	SourceInstanceID string
	Bundle           []byte
	Preview          *Preview
	Report           *ImportReport
	CreatedAt        time.Time
	CommittedAt      *time.Time
}

// SourceRecord is one portability_source_records row: the Source Identity
// entry that marks a bundle record as imported and remembers which row in
// this instance it became. (sourceInstanceID, sourceRecordID) is unique
// instance-wide — deliberately not workspace-scoped — so re-importing the
// same bundle classifies records as skipped/conflict instead of copying them
// again.
type SourceRecord struct {
	WorkspaceID        string
	SourceInstanceID   string
	SourceRecordID     string
	TargetKind         string // todo|channel|delivery
	TargetID           string
	ContentFingerprint string
}

// TodoImportRequest carries one bundle todo record for the todo importer; the
// cmd shim maps it onto the todo module's ImportTodoRequest, adding the
// principal's identity. Version carries the restored row's optimistic-lock
// version: the bundle wire shape does not export the source row's version, so
// the confirm handler always sends 0 — a restored row starts its own version
// history in this instance.
type TodoImportRequest struct {
	Title           string
	Description     *string
	DueAtUTC        *time.Time
	TimezoneAtInput *string
	Status          string
	ReminderVersion int
	Version         int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	CompletedAt     *time.Time
	DeletedAt       *time.Time
}

// ChannelImportRequest carries one bundle channel preference for the channel
// importer; imported channels are always unverified. The cmd shim maps it
// onto the identity module's ImportChannelHandler, adding the principal.
type ChannelImportRequest struct {
	Kind    string
	Address string
	Enabled bool
}

// DeliveryImportRequest carries one bundle delivery history row for the
// delivery importer; TodoID is the todo id resolved by the confirm handler
// (created in the same run, or previously registered in the source records).
// SourceInstanceID/SourceRecordID identify the delivery's own source record
// and form the reminder module's import idempotency key.
type DeliveryImportRequest struct {
	TodoID              string
	TodoReminderVersion int
	Channel             string
	State               string
	SuppressionReason   *string
	ProviderMessageID   *string
	LastErrorCode       *string
	AttemptCount        int
	TodoTitleSnapshot   string
	ScheduledAt         time.Time
	CreatedAt           time.Time
	SubmittedAt         *time.Time
	FinalizedAt         *time.Time
	ReceiptState        *string
	ReceiptErrorCode    *string
	ReceiptAt           *time.Time
	SourceInstanceID    string
	SourceRecordID      string
}

// ImportView is the read-side rendering of one import row. A pending row
// past its TTL renders State = expired without mutating the stored row.
type ImportView struct {
	ImportID         string        `json:"importId"`
	State            string        `json:"state"`
	SourceInstanceID string        `json:"sourceInstanceId"`
	Preview          Preview       `json:"preview"`
	Report           *ImportReport `json:"report,omitempty"`
	CreatedAt        time.Time     `json:"createdAt"`
	CommittedAt      *time.Time    `json:"committedAt,omitempty"`
}

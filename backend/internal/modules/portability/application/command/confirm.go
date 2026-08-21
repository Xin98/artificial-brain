package command

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// ConfirmImportHandler executes a previously uploaded import exactly once.
// It re-parses the stored bundle bytes and re-decides from scratch — the
// stored preview is never trusted — then executes the new records inside ONE
// unit-of-work transaction in the fixed order channels → todos → deliveries,
// registers one source record per imported row, and commits the import with
// the final report. The handler never begins a transaction itself: cmd
// injects the joinable UoW so the whole confirm joins one transaction.
type ConfirmImportHandler struct {
	Imports    ports.ImportStore
	Sources    ports.SourceRecordStore
	Parser     ports.BundleParser
	Todos      ports.TodoImporter
	Channels   ports.ChannelImporter
	Deliveries ports.DeliveryImporter
	UoW        ports.UnitOfWork
	Log        *slog.Logger
	NewID      func() string // reserved: the importer seams own their new row ids
	Now        func() time.Time
	ImportTTL  time.Duration
}

// Handle resolves the import row, enforces the committed/expired guards, and
// executes the re-decided bundle. A committed import reports
// domain.ErrImportConflict; a pending row created before now-TTL reports
// domain.ErrImportExpired. Importer failures surface unchanged and leave the
// row pending; only a successful transaction commits the row.
func (h *ConfirmImportHandler) Handle(ctx context.Context, principal ports.Principal, importID string) (dto.ImportReport, error) {
	row, err := h.Imports.Get(ctx, principal.WorkspaceID, importID)
	if err != nil {
		return dto.ImportReport{}, err
	}
	if row.State == dto.ImportStateCommitted {
		return dto.ImportReport{}, domain.ErrImportConflict
	}
	now := h.Now()
	if row.CreatedAt.Before(now.Add(-h.ImportTTL)) {
		return dto.ImportReport{}, domain.ErrImportExpired
	}

	parsed, err := h.Parser.Parse(row.Bundle)
	if err != nil {
		return dto.ImportReport{}, err
	}
	sourceInstanceID := parsed.Manifest.SourceInstanceID
	existing, err := h.Sources.Fingerprints(ctx, sourceInstanceID, allRecordIDs(parsed))
	if err != nil {
		return dto.ImportReport{}, err
	}
	plan := classifyBundle(parsed, existing)

	previousTodoIDs, err := h.previouslyImportedTodoIDs(ctx, sourceInstanceID, plan)
	if err != nil {
		return dto.ImportReport{}, err
	}

	if err := h.UoW.Run(ctx, func(ctx context.Context) error {
		return h.execute(ctx, principal, sourceInstanceID, plan, previousTodoIDs)
	}); err != nil {
		return dto.ImportReport{}, err
	}

	report := buildReport(plan.decisions, now)
	if err := h.Imports.Commit(ctx, principal.WorkspaceID, importID, report, now); err != nil {
		return dto.ImportReport{}, err
	}
	h.Log.Info("portability: import committed",
		slog.String("workspaceId", principal.WorkspaceID),
		slog.String("importId", importID),
		slog.String("sourceInstanceId", sourceInstanceID),
		slog.Int("new", report.New),
		slog.Int("skipped", report.Skipped),
		slog.Int("conflicts", report.Conflicts),
		slog.Int("invalid", report.Invalid),
	)
	return report, nil
}

// previouslyImportedTodoIDs resolves, keyed by source record id, the todo
// targets a previous import already registered — the delivery side of "this
// run's source-record mapping or already registered in Sources".
func (h *ConfirmImportHandler) previouslyImportedTodoIDs(ctx context.Context, sourceInstanceID string, plan *bundlePlan) (map[string]string, error) {
	targets := map[string]string{}
	seen := map[string]bool{}
	referenced := []string{}
	for _, record := range plan.deliveries {
		if record.SourceTodoRecordID == "" || seen[record.SourceTodoRecordID] {
			continue
		}
		seen[record.SourceTodoRecordID] = true
		referenced = append(referenced, record.SourceTodoRecordID)
	}
	if len(referenced) == 0 {
		return targets, nil
	}
	byKey, err := h.Sources.Targets(ctx, sourceInstanceID, referenced)
	if err != nil {
		return nil, err
	}
	for _, sourceRecordID := range referenced {
		if targetID, ok := byKey[sourceInstanceID+":"+sourceRecordID]; ok {
			targets[sourceRecordID] = targetID
		}
	}
	return targets, nil
}

// execute runs the decided records inside the caller's transaction in the
// fixed order channels → todos → deliveries and registers one source record
// per imported row. Importer failures return the error so the whole
// transaction rolls back; a duplicate channel downgrades to skipped and an
// orphan delivery becomes invalid without aborting.
func (h *ConfirmImportHandler) execute(ctx context.Context, principal ports.Principal, sourceInstanceID string, plan *bundlePlan, previousTodoIDs map[string]string) error {
	createdTodoIDs := map[string]string{}

	// 1. Channels first: unverified contact channels never block the rest.
	for _, record := range plan.channels {
		if plan.outcomeOf(domain.KindChannel, record.ID) != domain.OutcomeNew {
			continue
		}
		targetID, err := h.Channels.ImportChannel(ctx, principal, dto.ChannelImportRequest{
			Kind:    record.Kind,
			Address: record.Address,
			Enabled: record.Enabled,
		})
		if err != nil {
			if !errors.Is(err, ports.ErrChannelExists) {
				return err
			}
			// Duplicate (user, kind, address): the seam returns the existing
			// channel's id alongside the sentinel — downgrade to skipped and
			// register the source record against that existing target so a
			// re-import classifies the record instead of retrying it.
			plan.reviseDecision(domain.KindChannel, record.ID, domain.OutcomeSkipped, "channel already exists")
		}
		if err := h.Sources.Register(ctx, newSourceRecord(principal.WorkspaceID, sourceInstanceID, record.ID, domain.KindChannel, targetID, plan.fingerprints[record.ID])); err != nil {
			return err
		}
	}

	// 2. Todos: restored exactly as recorded, never planned.
	for _, record := range plan.todos {
		if plan.outcomeOf(domain.KindTodo, record.ID) != domain.OutcomeNew {
			continue
		}
		targetID, err := h.Todos.ImportTodo(ctx, principal, todoImportRequest(record))
		if err != nil {
			return err
		}
		if err := h.Sources.Register(ctx, newSourceRecord(principal.WorkspaceID, sourceInstanceID, record.ID, domain.KindTodo, targetID, plan.fingerprints[record.ID])); err != nil {
			return err
		}
		createdTodoIDs[record.ID] = targetID
	}

	// 3. Deliveries last: every todo they can reference exists by now.
	for _, record := range plan.deliveries {
		if plan.outcomeOf(domain.KindDelivery, record.ID) != domain.OutcomeNew {
			continue
		}
		todoID, ok := createdTodoIDs[record.SourceTodoRecordID]
		if !ok {
			todoID, ok = previousTodoIDs[record.SourceTodoRecordID]
		}
		if !ok {
			// Orphan delivery: its todo was neither created in this run nor
			// registered by a previous import. Skip it, never fail the run.
			plan.reviseDecision(domain.KindDelivery, record.ID, domain.OutcomeInvalid, "todo_not_found")
			continue
		}
		if err := h.Deliveries.ImportDelivery(ctx, principal, deliveryImportRequest(record, todoID, sourceInstanceID)); err != nil {
			return err
		}
		if err := h.Sources.Register(ctx, newSourceRecord(principal.WorkspaceID, sourceInstanceID, record.ID, domain.KindDelivery, deliveryImportKey(sourceInstanceID, record.ID), plan.fingerprints[record.ID])); err != nil {
			return err
		}
	}
	return nil
}

func newSourceRecord(workspaceID, sourceInstanceID, sourceRecordID, targetKind, targetID, fingerprint string) dto.SourceRecord {
	return dto.SourceRecord{
		WorkspaceID:        workspaceID,
		SourceInstanceID:   sourceInstanceID,
		SourceRecordID:     sourceRecordID,
		TargetKind:         targetKind,
		TargetID:           targetID,
		ContentFingerprint: fingerprint,
	}
}

// deliveryImportKey is the target id registered for an imported delivery.
// The reminder import seam generates its own row id and returns none, so the
// registration carries the delivery's import idempotency key instead (D4:
// "import:<sourceInstanceId>:<sourceRecordId>") — the unique, deterministic
// handle under which the reminder module stores the imported row.
func deliveryImportKey(sourceInstanceID, sourceRecordID string) string {
	return "import:" + sourceInstanceID + ":" + sourceRecordID
}

// todoImportRequest mirrors the bundle record into the todo importer's
// request. Version is always 0: the bundle wire shape carries no
// optimistic-lock version, so a restored row starts its own version history
// in this instance.
func todoImportRequest(record domain.TodoRecord) dto.TodoImportRequest {
	return dto.TodoImportRequest{
		Title:           record.Title,
		Description:     record.Description,
		DueAtUTC:        record.DueAtUTC,
		TimezoneAtInput: record.TimezoneAtInput,
		Status:          record.Status,
		ReminderVersion: record.ReminderVersion,
		Version:         0,
		CreatedAt:       record.CreatedAt,
		UpdatedAt:       record.UpdatedAt,
		CompletedAt:     record.CompletedAt,
		DeletedAt:       record.DeletedAt,
	}
}

// deliveryImportRequest mirrors the bundle record into the delivery
// importer's request. TodoReminderVersion is always 0: the bundle wire shape
// does not carry it and imported history is never re-planned.
func deliveryImportRequest(record domain.DeliveryRecord, todoID, sourceInstanceID string) dto.DeliveryImportRequest {
	return dto.DeliveryImportRequest{
		TodoID:              todoID,
		TodoReminderVersion: 0,
		Channel:             record.Channel,
		State:               record.State,
		SuppressionReason:   record.SuppressionReason,
		ProviderMessageID:   record.ProviderMessageID,
		LastErrorCode:       record.LastErrorCode,
		AttemptCount:        record.AttemptCount,
		TodoTitleSnapshot:   record.TodoTitleSnapshot,
		ScheduledAt:         record.ScheduledAt,
		CreatedAt:           record.CreatedAt,
		SubmittedAt:         record.SubmittedAt,
		FinalizedAt:         record.FinalizedAt,
		ReceiptState:        record.ReceiptState,
		ReceiptErrorCode:    record.ReceiptErrorCode,
		ReceiptAt:           record.ReceiptAt,
		SourceInstanceID:    sourceInstanceID,
		SourceRecordID:      record.ID,
	}
}

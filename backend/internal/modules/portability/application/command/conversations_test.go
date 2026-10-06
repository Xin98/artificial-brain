package command

import (
	"context"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

type historyFake struct {
	sessions     []dto.SessionExportRecord
	messages     []dto.MessageExportRecord
	principal    ports.Principal
	restored     []dto.MessageImportRequest
	messageError error
}

func (f *historyFake) ExportSessions(_ context.Context, p ports.Principal, offset, limit int) ([]dto.SessionExportRecord, error) {
	f.principal = p
	return pageHistory(f.sessions, offset, limit), nil
}
func (f *historyFake) ExportMessages(_ context.Context, p ports.Principal, offset, limit int) ([]dto.MessageExportRecord, error) {
	f.principal = p
	return pageHistory(f.messages, offset, limit), nil
}
func pageHistory[T any](records []T, offset, limit int) []T {
	if offset >= len(records) {
		return nil
	}
	end := offset + limit
	if end > len(records) {
		end = len(records)
	}
	return records[offset:end]
}
func (f *historyFake) ImportSession(_ context.Context, p ports.Principal, r dto.SessionImportRequest) (string, error) {
	f.principal = p
	f.sessions = append(f.sessions, r)
	return "restored-session", nil
}
func (f *historyFake) ImportMessage(_ context.Context, p ports.Principal, r dto.MessageImportRequest) (string, error) {
	if f.messageError != nil {
		return "", f.messageError
	}
	f.principal = p
	f.restored = append(f.restored, r)
	return "restored-message", nil
}

func TestMissingRestoredSessionProducesPartialReport(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("history"))
	session := domain.SessionRecord{ID: "s", Title: "已经删掉的会话", CreatedAt: exportedAt, UpdatedAt: exportedAt}
	rig.sources.fingerprints["instance-src:session:s"] = domain.Fingerprint(session)
	rig.sources.targets["instance-src:session:s"] = "missing-session"
	rig.handler.Conversations = &historyFake{messageError: ports.ErrConversationSessionNotFound}
	rig.parser.parsed = ports.ParsedBundle{Manifest: importManifest(), Sessions: []domain.SessionRecord{session}, Messages: []domain.MessageRecord{{ID: "1", SessionID: strPtr("s"), Role: "user", Body: "新增历史", Order: 1, CreatedAt: exportedAt}}}
	report, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.New != 0 || report.Skipped != 1 || report.Invalid != 1 {
		t.Fatalf("missing session report = %#v", report)
	}
}

func TestImportConversationRestoresInSourceOrderWithoutOperations(t *testing.T) {
	rig := newConfirmRig()
	rig.seedPendingImport([]byte("history"))
	history := &historyFake{}
	rig.handler.Conversations = history
	rig.parser.parsed = ports.ParsedBundle{Manifest: importManifest(), Sessions: []domain.SessionRecord{{ID: "s", Title: "会话", CreatedAt: exportedAt, UpdatedAt: exportedAt}}, Messages: []domain.MessageRecord{{ID: "2", SessionID: strPtr("s"), Role: "assistant", Body: "已删除", Order: 2, CreatedAt: exportedAt}, {ID: "1", SessionID: strPtr("s"), Role: "user", Body: "删除任务", Order: 1, CreatedAt: exportedAt}}}
	report, err := rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.New != 3 || len(history.restored) != 2 || history.restored[0].Body != "删除任务" || *history.restored[0].SessionID != "restored-session" || len(rig.todos.calls) != 0 || len(rig.deliveries.calls) != 0 {
		t.Fatalf("restore = %#v; report %#v", history, report)
	}
	for _, record := range rig.sources.registered {
		rig.sources.fingerprints[record.SourceInstanceID+":"+record.SourceRecordID] = record.ContentFingerprint
		rig.sources.targets[record.SourceInstanceID+":"+record.SourceRecordID] = record.TargetID
	}
	rig.seedPendingImport([]byte("history"))
	report, err = rig.handler.Handle(context.Background(), testPrincipal(), "import-1")
	if err != nil {
		t.Fatal(err)
	}
	if report.Skipped != 3 || len(history.restored) != 2 {
		t.Fatalf("retry duplicated history: %#v, %#v", history, report)
	}
}

func TestConversationDuplicateRecordsDoNotClaimSuccessfulRestore(t *testing.T) {
	session := domain.SessionRecord{ID: "same", Title: "会话", CreatedAt: exportedAt, UpdatedAt: exportedAt}
	message := domain.MessageRecord{ID: "same-message", Role: "user", Body: "历史", Order: 1, CreatedAt: exportedAt}
	plan := classifyBundle(ports.ParsedBundle{Manifest: importManifest(), Sessions: []domain.SessionRecord{session, session}, Messages: []domain.MessageRecord{message, message}}, nil)
	summary := summarizeDecisions(plan.decisions)
	if summary.New != 0 || summary.Invalid != 4 {
		t.Fatalf("duplicates claim successful restore: %#v", summary)
	}
}

func TestLegacySourceInSameWorkspaceReportsOwnershipConflict(t *testing.T) {
	todo := validTodoRecord("id", "已有待办")
	plan := classifyBundle(ports.ParsedBundle{Manifest: importManifest(), Todos: []domain.TodoRecord{todo}}, map[string]string{"instance-src:id": "legacy-owner-unverified"})
	if len(plan.decisions) != 1 || plan.decisions[0].Outcome != "conflict" || plan.decisions[0].Reason != "legacy_source_owner_unverified" {
		t.Fatalf("legacy decision = %#v", plan.decisions)
	}
}

func TestSourceInstanceWithColonRemainsIdempotent(t *testing.T) {
	manifest := importManifest()
	manifest.SourceInstanceID = "instance:source"
	session := domain.SessionRecord{ID: "s", Title: "会话", CreatedAt: exportedAt, UpdatedAt: exportedAt}
	plan := classifyBundle(ports.ParsedBundle{Manifest: manifest, Sessions: []domain.SessionRecord{session}}, map[string]string{"instance:source:session:s": domain.Fingerprint(session)})
	if plan.decisions[0].Outcome != "skipped" {
		t.Fatalf("source namespace ignored: %#v", plan.decisions)
	}
}

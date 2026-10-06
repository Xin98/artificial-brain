package archive

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

type roundtripImports struct {
	rows map[string]dto.ImportRecordRow
}

func (s *roundtripImports) Save(_ context.Context, r dto.ImportRecordRow) error {
	s.rows[r.ID] = r
	return nil
}
func (s *roundtripImports) Get(_ context.Context, workspace, id string) (dto.ImportRecordRow, error) {
	r, ok := s.rows[id]
	if !ok || r.WorkspaceID != workspace {
		return dto.ImportRecordRow{}, domain.ErrImportNotFound
	}
	return r, nil
}
func (s *roundtripImports) Commit(_ context.Context, workspace, id string, report dto.ImportReport, at time.Time) error {
	r := s.rows[id]
	r.State = dto.ImportStateCommitted
	r.Report = &report
	r.CommittedAt = &at
	s.rows[id] = r
	return nil
}

type roundtripSources struct {
	records map[string]dto.SourceRecord
	owner   ports.Principal
}

func (s *roundtripSources) ForOwner(p ports.Principal) ports.SourceRecordStore {
	return &roundtripSources{records: s.records, owner: p}
}
func (s *roundtripSources) key(source, id string) string {
	return s.owner.WorkspaceID + ":" + s.owner.UserID + ":" + source + ":" + id
}
func (s *roundtripSources) Fingerprints(_ context.Context, source string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if r, ok := s.records[s.key(source, id)]; ok {
			out[source+":"+id] = r.ContentFingerprint
		}
	}
	return out, nil
}
func (s *roundtripSources) Targets(_ context.Context, source string, ids []string) (map[string]string, error) {
	out := map[string]string{}
	for _, id := range ids {
		if r, ok := s.records[s.key(source, id)]; ok {
			out[source+":"+id] = r.TargetID
		}
	}
	return out, nil
}
func (s *roundtripSources) Register(_ context.Context, r dto.SourceRecord) error {
	s.records[s.key(r.SourceInstanceID, r.SourceRecordID)] = r
	return nil
}

type roundtripUoW struct{}

func (roundtripUoW) Run(ctx context.Context, work func(context.Context) error) error {
	return work(ctx)
}

type roundtripHistory struct {
	sessions []dto.SessionImportRequest
	messages []dto.MessageImportRequest
	owner    ports.Principal
}

func (s *roundtripHistory) ImportSession(_ context.Context, p ports.Principal, r dto.SessionImportRequest) (string, error) {
	s.owner = p
	s.sessions = append(s.sessions, r)
	return "restored-session", nil
}
func (s *roundtripHistory) ImportMessage(_ context.Context, p ports.Principal, r dto.MessageImportRequest) (string, error) {
	s.owner = p
	s.messages = append(s.messages, r)
	return "restored-message", nil
}

func TestExportUploadConfirmConversationRoundTripIsOwnerScopedAndIdempotent(t *testing.T) {
	ctx := context.Background()
	at := bundleExportedAt
	session := "source-session"
	source := &exportHistorySource{sessions: []dto.SessionExportRecord{{ID: session, Title: "历史操作", CreatedAt: at, UpdatedAt: at}}, messages: []dto.MessageExportRecord{{ID: "1", SessionID: &session, Role: "user", Body: "删除待办", Order: 1, CreatedAt: at}, {ID: "2", SessionID: &session, Role: "assistant", Body: "已删除\n保留原文", Order: 2, CreatedAt: at}}}
	export := command.ExportBundleHandler{Instance: source, Todos: source, Channels: source, Deliveries: source, Conversations: source, Archive: Factory(), PageSize: 1, Now: func() time.Time { return at }}
	owner := ports.Principal{WorkspaceID: "workspace", UserID: "owner"}
	var bundle bytes.Buffer
	if _, err := export.Handle(ctx, owner, &bundle); err != nil {
		t.Fatal(err)
	}
	imports := &roundtripImports{rows: map[string]dto.ImportRecordRow{}}
	sources := &roundtripSources{records: map[string]dto.SourceRecord{}}
	history := &roundtripHistory{}
	next := 0
	upload := command.UploadImportHandler{Imports: imports, Sources: sources, Parser: NewParser(), NewID: func() string { next++; return string(rune('a' + next)) }, Now: func() time.Time { return at }, ImportTTL: time.Hour}
	confirm := command.ConfirmImportHandler{Imports: imports, Sources: sources, Parser: NewParser(), Conversations: history, UoW: roundtripUoW{}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Now: func() time.Time { return at }, ImportTTL: time.Hour}
	for run := 0; run < 2; run++ {
		id, preview, err := upload.Handle(ctx, owner, bundle.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		report, err := confirm.Handle(ctx, owner, id)
		if err != nil {
			t.Fatal(err)
		}
		if run == 0 && (preview.New != 3 || report.New != 3) {
			t.Fatalf("first restore = %#v,%#v", preview, report)
		}
		if run == 1 && (preview.Skipped != 3 || report.Skipped != 3) {
			t.Fatalf("reimport = %#v,%#v", preview, report)
		}
	}
	if len(history.messages) != 2 || len(history.sessions) != 1 || history.messages[1].Body != source.messages[1].Body || history.messages[0].Role != "user" || *history.messages[0].SessionID != "restored-session" || history.owner != owner {
		t.Fatalf("history restore = %#v", history)
	}
	foreign := ports.Principal{WorkspaceID: "another-workspace", UserID: "owner"}
	id, preview, err := upload.Handle(ctx, foreign, bundle.Bytes())
	if err != nil || preview.New != 3 {
		t.Fatalf("foreign restore blocked by another tenant: %#v,%v", preview, err)
	}
	if _, err := confirm.Handle(ctx, owner, id); err != domain.ErrImportNotFound {
		t.Fatalf("foreign upload accessible: %v", err)
	}
}

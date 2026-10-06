package archive

import (
	"bytes"
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
)

type exportHistorySource struct {
	sessions  []dto.SessionExportRecord
	messages  []dto.MessageExportRecord
	principal ports.Principal
}

func (f *exportHistorySource) InstanceID(context.Context) (string, error) { return "source", nil }
func (f *exportHistorySource) ExportTodos(context.Context, string, string, int, int) ([]dto.TodoExportRecord, error) {
	return nil, nil
}
func (f *exportHistorySource) ExportDeliveries(context.Context, string, int, int) ([]dto.DeliveryExportRecord, error) {
	return nil, nil
}
func (f *exportHistorySource) ExportChannels(context.Context, ports.Principal) ([]dto.ChannelExportRecord, error) {
	return nil, nil
}
func (f *exportHistorySource) ExportSessions(_ context.Context, p ports.Principal, offset, limit int) ([]dto.SessionExportRecord, error) {
	f.principal = p
	return exportHistoryPage(f.sessions, offset, limit), nil
}
func (f *exportHistorySource) ExportMessages(_ context.Context, p ports.Principal, offset, limit int) ([]dto.MessageExportRecord, error) {
	f.principal = p
	return exportHistoryPage(f.messages, offset, limit), nil
}
func exportHistoryPage[T any](records []T, offset, limit int) []T {
	if offset >= len(records) {
		return nil
	}
	end := offset + limit
	if end > len(records) {
		end = len(records)
	}
	return records[offset:end]
}

func TestExportConversationRoundTripPreservesFullBodyAndOrder(t *testing.T) {
	at := bundleExportedAt
	id := "s-1"
	intent := "todo.delete"
	history := &exportHistorySource{sessions: []dto.SessionExportRecord{{ID: id, Title: "过去的会话", CreatedAt: at, UpdatedAt: at}}, messages: []dto.MessageExportRecord{{ID: "1", SessionID: &id, Role: "assistant", Body: "一段完整回复\n下一段", ResolvedIntent: &intent, Order: 1, CreatedAt: at}, {ID: "2", Role: "user", Body: "旧消息", Order: 2, CreatedAt: at}}}
	h := command.ExportBundleHandler{Instance: history, Todos: history, Channels: history, Deliveries: history, Conversations: history, Archive: Factory(), PageSize: 1, Now: func() time.Time { return at }}
	principal := ports.Principal{WorkspaceID: "workspace", UserID: "owner"}
	var output bytes.Buffer
	manifest, err := h.Handle(context.Background(), principal, &output)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := Parse(output.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SchemaVersion != "2" || manifest.Counts.Messages != 2 || manifest.Counts.Sessions != 1 || !reflect.DeepEqual(parsed.Messages, history.messages) || !reflect.DeepEqual(parsed.Sessions, history.sessions) {
		t.Fatalf("roundtrip lost history: %#v", parsed)
	}
	if history.principal != principal {
		t.Fatalf("owner scope = %#v", history.principal)
	}
	corrupted := rewriteZip(t, output.Bytes(), func(name string, content []byte) ([]byte, bool) {
		if name == dto.MessagesEntry {
			return []byte("[]"), true
		}
		return content, true
	})
	if _, err := Parse(corrupted); err == nil {
		t.Fatal("tampered conversation history accepted")
	}
}

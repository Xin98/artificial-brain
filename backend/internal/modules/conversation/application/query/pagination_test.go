package query

import (
	"encoding/json"
	"strconv"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

func TestSessionOverflowProvidesNextOffset(t *testing.T) {
	sessions := newFakeSessionStore()
	for i := 0; i <= MaxListedSessions; i++ {
		sessions.seed(domain.Session{ID: strconv.Itoa(i), WorkspaceID: "ws-1", UserID: "user-1"})
	}
	h := &ListSessionsHandler{Sessions: sessions}
	got, err := h.Handle(ctx(), "ws-1", "user-1")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var view map[string]any
	_ = json.Unmarshal(raw, &view)
	if len(got.Sessions) != MaxListedSessions || view["hasMore"] != true || view["nextOffset"] != float64(MaxListedSessions) {
		t.Fatalf("page = %s", raw)
	}
}

func TestHistoryOverflowKeepsBoundaryAndProvidesCursor(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.seed(domain.Session{ID: "s-1", WorkspaceID: "ws-1", UserID: "user-1"})
	log := &fakeMessageLog{}
	for i := 1; i <= MaxHistoryMessages+1; i++ {
		log.entries = append(log.entries, ports.MessageLogEntry{ID: strconv.Itoa(i), SessionID: "s-1"})
	}
	h := &GetHistoryHandler{Sessions: sessions, Messages: log}
	got, err := h.Handle(ctx(), "ws-1", "user-1", "s-1")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	var view map[string]any
	_ = json.Unmarshal(raw, &view)
	if len(got.Messages) != MaxHistoryMessages || got.Messages[0].ID != "2" || view["hasMore"] != true || view["nextBefore"] != "2" {
		t.Fatalf("history boundary/cursor = %v / %v / %v", got.Messages[0].ID, view["hasMore"], view["nextBefore"])
	}
}

func TestHistoryPagesRecoverEveryMessageExactlyOnce(t *testing.T) {
	sessions := newFakeSessionStore()
	sessions.seed(domain.Session{ID: "s-1", WorkspaceID: "ws-1", UserID: "user-1"})
	log := &fakeMessageLog{}
	for i := 1; i <= 450; i++ {
		log.entries = append(log.entries, ports.MessageLogEntry{ID: strconv.Itoa(i), SessionID: "s-1"})
	}
	h := &GetHistoryHandler{Sessions: sessions, Messages: log}
	seen := map[string]bool{}
	before := ""
	for page := 0; page < 3; page++ {
		got, err := h.HandlePage(ctx(), "ws-1", "user-1", "s-1", before)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range got.Messages {
			if seen[row.ID] {
				t.Fatalf("duplicate row %s", row.ID)
			}
			seen[row.ID] = true
		}
		if page < 2 && !got.HasMore {
			t.Fatal("lost older page")
		}
		if page == 2 && (got.HasMore || got.NextBefore != "") {
			t.Fatal("last page advertises more")
		}
		before = got.NextBefore
	}
	if len(seen) != 450 {
		t.Fatalf("recovered %d, want 450", len(seen))
	}
}

func TestSessionPagesRetainScopedRemainder(t *testing.T) {
	sessions := newFakeSessionStore()
	for i := 0; i < 230; i++ {
		sessions.seed(domain.Session{ID: strconv.Itoa(i), WorkspaceID: "ws-1", UserID: "user-1"})
	}
	sessions.seed(domain.Session{ID: "foreign", WorkspaceID: "ws-2", UserID: "user-1"})
	h := &ListSessionsHandler{Sessions: sessions}
	seen := map[string]bool{}
	for offset := 0; ; {
		got, err := h.HandlePage(ctx(), "ws-1", "user-1", offset)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range got.Sessions {
			if seen[row.ID] || row.ID == "foreign" {
				t.Fatalf("invalid row %s", row.ID)
			}
			seen[row.ID] = true
		}
		if !got.HasMore {
			if got.NextOffset != nil {
				t.Fatal("last page has offset")
			}
			break
		}
		if got.NextOffset == nil || *got.NextOffset <= offset {
			t.Fatal("page did not advance")
		}
		offset = *got.NextOffset
	}
	if len(seen) != 230 {
		t.Fatalf("sessions recovered = %d", len(seen))
	}
}

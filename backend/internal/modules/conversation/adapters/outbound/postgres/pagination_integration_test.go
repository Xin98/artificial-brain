package postgres

import (
	"context"
	"strconv"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

func TestScopedStorePaginationKeepsExclusiveMessageBoundary(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	sessions, messages := NewSessionStore(pool), NewMessageLogStore(pool)
	workspace, user := randomID(t), randomID(t)
	first := newSession(t, randomID(t), workspace, user, "first", testNow)
	second := newSession(t, randomID(t), workspace, user, "second", testNow)
	foreign := newSession(t, randomID(t), workspace, randomID(t), "foreign", testNow)
	if err := sessions.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := sessions.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	if err := sessions.Create(ctx, foreign); err != nil {
		t.Fatal(err)
	}
	page, err := sessions.ListPage(ctx, workspace, user, 0, 1)
	if err != nil || len(page) != 1 {
		t.Fatalf("first session page: %#v, %v", page, err)
	}
	next, err := sessions.ListPage(ctx, workspace, user, 1, 1)
	if err != nil || len(next) != 1 || next[0].ID == page[0].ID || next[0].ID == foreign.ID {
		t.Fatalf("next scoped page: %#v, %v", next, err)
	}
	for i := 1; i <= 7; i++ {
		if err := messages.Append(ctx, ports.MessageLog{WorkspaceID: workspace, UserID: user, SessionID: &first.ID, Role: ports.RoleUser, Body: strconv.Itoa(i), CreatedAt: testNow}); err != nil {
			t.Fatal(err)
		}
	}
	latest, err := messages.ListBefore(ctx, workspace, user, first.ID, "", 3)
	if err != nil || len(latest) != 3 || latest[0].Body != "5" {
		t.Fatalf("latest page: %#v, %v", latest, err)
	}
	older, err := messages.ListBefore(ctx, workspace, user, first.ID, latest[0].ID, 3)
	if err != nil || len(older) != 3 || older[0].Body != "2" || older[2].Body != "4" {
		t.Fatalf("older page: %#v, %v", older, err)
	}
	for _, scope := range [][2]string{{randomID(t), user}, {workspace, randomID(t)}} {
		rows, err := messages.ListBefore(ctx, scope[0], scope[1], first.ID, latest[0].ID, 3)
		if err != nil || len(rows) != 0 {
			t.Fatalf("foreign page leaked: %#v, %v", rows, err)
		}
	}
}

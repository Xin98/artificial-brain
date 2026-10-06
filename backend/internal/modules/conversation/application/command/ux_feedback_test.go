package command

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

func TestListTranscriptRetainsTodoDetails(t *testing.T) {
	due := fixedNow
	model := &fakeModel{turn: rawTurn("好的", `{"schemaVersion":"1","intent":"todo.list","arguments":{},"confidence":0.95,"missingFields":[]}`)}
	gateway := &fakeTodoGateway{listedTodos: []tododto.Todo{{ID: "task-1", Title: "周报", Status: "completed", DueAtUTC: &due}, {ID: "task-2", Title: "采购", Status: "pending"}}}
	log := &fakeMessageLog{}
	h := newProcessHandler(model, gateway, newFakeConfirmationStore(), log, newFakeSessionStore())
	if _, err := h.Handle(ctx(), "ws-1", "user-1", "", "列出待办", "UTC"); err != nil {
		t.Fatal(err)
	}
	body := log.messages[1].Body
	for _, want := range []string{"周报", "completed", "2026-08-18", "采购", "pending"} {
		if !strings.Contains(body, want) {
			t.Fatalf("history %q missing %q", body, want)
		}
	}
}

func TestConfirmationSupportsSessionOutcome(t *testing.T) {
	store := newFakeConfirmationStore()
	seedConfirmation(t, store, 5*time.Minute)
	h := newConfirmActionHandler(&fakeTodoGateway{gottenTodo: tododto.Todo{ID: "todo-1", Title: "周报", Version: 3}}, store)
	_, ok := any(h).(interface {
		HandleWithSession(context.Context, string, string, string, string) (dto.MessageResponse, error)
	})
	if !ok {
		t.Fatal("confirmation has no session-scoped completion path")
	}
}

type outcomeLog struct {
	fakeMessageLog
	inTransaction *bool
	appendErr     error
}

func (l *outcomeLog) Append(ctx context.Context, row ports.MessageLog) error {
	if !*l.inTransaction {
		return errors.New("append outside transaction")
	}
	if l.appendErr != nil {
		return l.appendErr
	}
	return l.fakeMessageLog.Append(ctx, row)
}

type outcomeUoW struct {
	active  bool
	store   *fakeConfirmationStore
	gateway *fakeTodoGateway
}

func (u *outcomeUoW) Run(ctx context.Context, work func(context.Context) error) error {
	u.active = true
	defer func() { u.active = false }()
	err := work(ctx)
	if err != nil {
		u.store.consumedAt = map[string]time.Time{}
		u.gateway.deleteRequests = nil
	}
	return err
}

func TestConfirmationOutcomeIsScopedAndTransactional(t *testing.T) {
	for _, scenario := range []string{"success", "foreign session", "append failure"} {
		t.Run(scenario, func(t *testing.T) {
			store := newFakeConfirmationStore()
			seedConfirmation(t, store, 5*time.Minute)
			gateway := &fakeTodoGateway{gottenTodo: tododto.Todo{ID: "todo-1", Title: "周报", Version: 3}}
			sessions := newFakeSessionStore()
			seedSession(t, sessions, "s-1")
			if scenario == "foreign session" {
				foreign := sessions.sessions["s-1"]
				foreign.UserID = "other-user"
				sessions.sessions["s-1"] = foreign
			}
			uow := &outcomeUoW{store: store, gateway: gateway}
			log := &outcomeLog{inTransaction: &uow.active}
			if scenario == "append failure" {
				log.appendErr = errors.New("transcript unavailable")
			}
			h := newConfirmActionHandler(gateway, store)
			h.Sessions, h.Messages, h.UoW = sessions, log, uow
			got, err := h.HandleWithSession(ctx(), "ws-1", "user-1", "conf-1", "s-1")
			if scenario == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if got.SessionID != "s-1" || len(log.messages) != 1 || log.messages[0].Body != "已删除待办「周报」。" || *log.messages[0].SessionID != "s-1" {
					t.Fatalf("outcome = %#v; rows = %#v", got, log.messages)
				}
				if !sessions.sessions["s-1"].UpdatedAt.Equal(fixedNow) {
					t.Fatal("session activity not updated")
				}
			} else {
				if err == nil {
					t.Fatal("expected error")
				}
				if scenario == "foreign session" && !errors.Is(err, domain.ErrSessionNotFound) {
					t.Fatalf("ownership error = %v", err)
				}
				if len(store.consumedAt) != 0 || len(gateway.deleteRequests) != 0 || len(log.messages) != 0 {
					t.Fatal("failed outcome left partial state")
				}
			}
		})
	}
}

func TestSingleDeleteConfirmationIncludesTargetDetails(t *testing.T) {
	due := fixedNow
	model := &fakeModel{turn: rawTurn("好的", `{"schemaVersion":"1","intent":"todo.delete","arguments":{"keyword":"周报"},"confidence":0.95,"missingFields":[]}`)}
	gateway := &fakeTodoGateway{candidates: []tododto.Candidate{{TodoID: "task-1", Title: "周报", DueAtUTC: &due, Version: 1}}}
	h := newProcessHandler(model, gateway, newFakeConfirmationStore(), &fakeMessageLog{}, newFakeSessionStore())
	got, err := h.Handle(ctx(), "ws-1", "user-1", "", "删除周报", "UTC")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Title != "周报" || got.Candidates[0].DueAtUTC == nil {
		t.Fatalf("target missing: %#v", got)
	}
}

func TestCreateTranscriptNeverClaimsDeliveryWithoutScheduling(t *testing.T) {
	model := &fakeModel{turn: rawTurn("好的", createProposal)}
	gateway := &fakeTodoGateway{createdTodo: tododto.Todo{ID: "task-1", Title: "提交周报", Status: "pending"}}
	log := &fakeMessageLog{}
	h := newProcessHandler(model, gateway, newFakeConfirmationStore(), log, newFakeSessionStore())
	if _, err := h.Handle(ctx(), "ws-1", "user-1", "", "明天提醒", "UTC"); err != nil {
		t.Fatal(err)
	}
	if log.messages[1].Role != ports.RoleAssistant || !strings.Contains(log.messages[1].Body, "未安排提醒") {
		t.Fatalf("misleading history: %q", log.messages[1].Body)
	}
}

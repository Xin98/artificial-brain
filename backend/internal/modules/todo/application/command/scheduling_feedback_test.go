package command

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
)

func TestCreateSchedulingFeedbackReflectsChannels(t *testing.T) {
	for _, channels := range [][]string{nil, {"email"}} {
		store, planner := newFakeTodoStore(), newFakePlanner()
		h := newCreateHandler(store, planner, func(context.Context, string, string) ([]string, error) { return channels, nil })
		got, err := h.Handle(ctx(), dto.CreateTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", Title: "task", DueAtUTC: dueAt(fixedNow)})
		if err != nil {
			t.Fatal(err)
		}
		assertSchedulingFeedback(t, got, len(channels) > 0, len(channels))
	}
}

func TestUpdateSchedulingFeedbackReflectsRescheduleAndClear(t *testing.T) {
	for _, clear := range []bool{false, true} {
		store, planner := newFakeTodoStore(), newFakePlanner()
		seedTodo(t, store, "todo-1", dueAt(fixedNow))
		h := newUpdateHandler(store, planner, nil)
		due := dueAt(fixedNow.AddDate(0, 0, 1))
		if clear {
			due = nil
		}
		got, err := h.Handle(ctx(), dto.UpdateTodoRequest{WorkspaceID: "ws-1", UserID: "user-1", TodoID: "todo-1", Version: 1, DueChanged: true, DueAtUTC: due})
		if err != nil {
			t.Fatal(err)
		}
		assertSchedulingFeedback(t, got, false, 0)
	}
}

func assertSchedulingFeedback(t *testing.T, got dto.Todo, scheduled bool, channelCount int) {
	t.Helper()
	raw, _ := json.Marshal(got)
	var view map[string]any
	if err := json.Unmarshal(raw, &view); err != nil {
		t.Fatal(err)
	}
	if actual, ok := view["reminderScheduled"].(bool); !ok || actual != scheduled {
		t.Fatalf("reminderScheduled = %v, want %v; response %s", view["reminderScheduled"], scheduled, raw)
	}
	if actual, ok := view["reminderChannels"].([]any); !ok || len(actual) != channelCount {
		t.Fatalf("reminderChannels = %v, want %d channels", view["reminderChannels"], channelCount)
	}
}

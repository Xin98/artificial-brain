package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/todo/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
)

var testNow = time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

func setupTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	directory := filepath.Join("..", "..", "..", "..", "..", "..", "..", "deploy", "migrations")
	if err := database.RunMigrations(ctx, url, directory); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	pool, err := database.OpenPool(ctx, url)
	if err != nil {
		t.Fatalf("OpenPool() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `truncate todo.todos, reminder.reminder_plans cascade`); err != nil {
		t.Fatalf("truncate error = %v", err)
	}
	return pool
}

func randomID(t *testing.T) string {
	t.Helper()
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func newTodo(t *testing.T, workspaceID, ownerUserID, title string, due *time.Time) domain.Todo {
	t.Helper()
	todo, err := domain.New(randomID(t), workspaceID, ownerUserID, title, nil, due, nil, testNow)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return todo
}

func TestStoreInsertGetRoundTripIncludingNullables(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	undated := newTodo(t, workspaceID, ownerUserID, "无期任务", nil)
	if err := store.Insert(ctx, undated); err != nil {
		t.Fatalf("Insert(undated) error = %v", err)
	}
	got, err := store.Get(ctx, workspaceID, ownerUserID, undated.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Title != "无期任务" || got.DueAtUTC != nil || got.Description != nil || got.Status != domain.StatusPending {
		t.Fatalf("round trip = %#v", got)
	}
	if got.Version != 1 || got.ReminderVersion != 1 {
		t.Fatalf("versions = %d/%d", got.Version, got.ReminderVersion)
	}

	due := testNow.Add(3 * time.Hour)
	description := "带说明"
	dated, err := domain.New(randomID(t), workspaceID, ownerUserID, "有期任务", &description, &due, nil, testNow)
	if err != nil {
		t.Fatalf("New(dated) error = %v", err)
	}
	if err := store.Insert(ctx, dated); err != nil {
		t.Fatalf("Insert(dated) error = %v", err)
	}
	got, err = store.Get(ctx, workspaceID, ownerUserID, dated.ID)
	if err != nil {
		t.Fatalf("Get(dated) error = %v", err)
	}
	if got.DueAtUTC == nil || !got.DueAtUTC.Equal(due) || got.Description == nil || *got.Description != description {
		t.Fatalf("dated round trip = %#v", got)
	}
}

func TestStoreUpdateEnforcesOptimisticConcurrency(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	todo := newTodo(t, workspaceID, ownerUserID, "原版", nil)
	if err := store.Insert(ctx, todo); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if err := todo.Complete(1, testNow); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := store.Update(ctx, todo, 99); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Update(stale) error = %v, want ErrConflict", err)
	}
	if err := store.Update(ctx, todo, 1); err != nil {
		t.Fatalf("Update(current) error = %v", err)
	}
	got, err := store.Get(ctx, workspaceID, ownerUserID, todo.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != domain.StatusCompleted || got.Version != 2 || got.CompletedAt == nil {
		t.Fatalf("stored after update = %#v", got)
	}
}

func TestStoreListAppliesCombinableFiltersAndExcludesDeleted(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	soon := testNow.Add(time.Hour)
	far := testNow.Add(72 * time.Hour)
	report := newTodo(t, workspaceID, ownerUserID, "提交周报", &soon)
	review := newTodo(t, workspaceID, ownerUserID, "代码评审", &far)
	undated := newTodo(t, workspaceID, ownerUserID, "整理文档", nil)
	deleted := newTodo(t, workspaceID, ownerUserID, "已删除周报", &soon)
	if err := deleted.Delete(1, testNow); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	for _, todo := range []domain.Todo{report, review, undated, deleted} {
		if err := store.Insert(ctx, todo); err != nil {
			t.Fatalf("Insert(%s) error = %v", todo.Title, err)
		}
	}

	all, err := store.List(ctx, workspaceID, ownerUserID, dto.ListFilters{}, dto.MaxListLimit)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("List(all) = %d, want 3 (deleted excluded)", len(all))
	}

	keyword, err := store.List(ctx, workspaceID, ownerUserID, dto.ListFilters{Keyword: "周报"}, dto.MaxListLimit)
	if err != nil || len(keyword) != 1 || keyword[0].Title != "提交周报" {
		t.Fatalf("List(keyword) = %#v, err = %v", keyword, err)
	}

	statusFiltered, err := store.List(ctx, workspaceID, ownerUserID, dto.ListFilters{Status: "pending"}, dto.MaxListLimit)
	if err != nil || len(statusFiltered) != 3 {
		t.Fatalf("List(status) = %d, err = %v", len(statusFiltered), err)
	}

	dueFrom := testNow.Add(2 * time.Hour)
	dueTo := testNow.Add(100 * time.Hour)
	window, err := store.List(ctx, workspaceID, ownerUserID, dto.ListFilters{DueFrom: &dueFrom, DueTo: &dueTo}, dto.MaxListLimit)
	if err != nil || len(window) != 1 || window[0].Title != "代码评审" {
		t.Fatalf("List(due window) = %#v, err = %v", window, err)
	}

	noDue, err := store.List(ctx, workspaceID, ownerUserID, dto.ListFilters{NoDue: true}, dto.MaxListLimit)
	if err != nil || len(noDue) != 1 || noDue[0].Title != "整理文档" {
		t.Fatalf("List(noDue) = %#v, err = %v", noDue, err)
	}

	combined, err := store.List(ctx, workspaceID, ownerUserID, dto.ListFilters{Keyword: "周报", NoDue: true}, dto.MaxListLimit)
	if err != nil || len(combined) != 0 {
		t.Fatalf("List(keyword+noDue) = %#v, err = %v, want empty", combined, err)
	}
}

func TestStoreDashboardCountsAgainstSeededFixtures(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	dayStart := time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)
	dayEnd := time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	overdue := newTodo(t, workspaceID, ownerUserID, "逾期", ptrTime(now.Add(-2*time.Hour)))
	dueToday := newTodo(t, workspaceID, ownerUserID, "今天", ptrTime(now.Add(time.Hour)))
	tomorrow := newTodo(t, workspaceID, ownerUserID, "明天", ptrTime(dayEnd.Add(5*time.Hour)))
	undated := newTodo(t, workspaceID, ownerUserID, "无期", nil)
	completed := newTodo(t, workspaceID, ownerUserID, "完成", ptrTime(now.Add(-time.Hour)))
	if err := completed.Complete(1, now.Add(-30*time.Minute)); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	oldCompleted := newTodo(t, workspaceID, ownerUserID, "上周完成", nil)
	if err := oldCompleted.Complete(1, now.Add(-8*24*time.Hour)); err != nil {
		t.Fatalf("Complete(old) error = %v", err)
	}
	deleted := newTodo(t, workspaceID, ownerUserID, "删除", ptrTime(now))
	if err := deleted.Delete(1, testNow); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	for _, todo := range []domain.Todo{overdue, dueToday, tomorrow, undated, completed, oldCompleted, deleted} {
		if err := store.Insert(ctx, todo); err != nil {
			t.Fatalf("Insert(%s) error = %v", todo.Title, err)
		}
	}

	summary, err := store.Dashboard(ctx, workspaceID, ownerUserID, now, dayStart, dayEnd)
	if err != nil {
		t.Fatalf("Dashboard() error = %v", err)
	}
	if summary.PendingTotal != 4 {
		t.Fatalf("PendingTotal = %d, want 4", summary.PendingTotal)
	}
	// dueToday overlaps overdue: the overdue todo's due also falls inside
	// today's window.
	if summary.DueToday != 2 {
		t.Fatalf("DueToday = %d, want 2", summary.DueToday)
	}
	if summary.Overdue != 1 {
		t.Fatalf("Overdue = %d, want 1", summary.Overdue)
	}
	if summary.NoDue != 1 {
		t.Fatalf("NoDue = %d, want 1", summary.NoDue)
	}
	if summary.CompletedLast7Days != 1 {
		t.Fatalf("CompletedLast7Days = %d, want 1 (older completion excluded)", summary.CompletedLast7Days)
	}
}

func TestStoreSearchCandidatesILikeAndLimit(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewStore(pool)
	workspaceID, ownerUserID := randomID(t), randomID(t)

	for index := 0; index < 12; index++ {
		todo := newTodo(t, workspaceID, ownerUserID, fmt.Sprintf("周报任务%d", index), nil)
		if err := store.Insert(ctx, todo); err != nil {
			t.Fatalf("Insert() error = %v", err)
		}
	}
	other := newTodo(t, workspaceID, ownerUserID, "其他事项", nil)
	if err := store.Insert(ctx, other); err != nil {
		t.Fatalf("Insert(other) error = %v", err)
	}
	completed := newTodo(t, workspaceID, ownerUserID, "周报已完成", nil)
	if err := completed.Complete(1, testNow); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := store.Insert(ctx, completed); err != nil {
		t.Fatalf("Insert(completed) error = %v", err)
	}

	candidates, err := store.SearchCandidates(ctx, workspaceID, ownerUserID, "周报", dto.MaxCandidateLimit)
	if err != nil {
		t.Fatalf("SearchCandidates() error = %v", err)
	}
	if len(candidates) != dto.MaxCandidateLimit {
		t.Fatalf("candidates = %d, want capped %d", len(candidates), dto.MaxCandidateLimit)
	}
	for _, candidate := range candidates {
		if candidate.Version != 1 || candidate.Title == "周报已完成" {
			t.Fatalf("candidate = %#v, want pending with version", candidate)
		}
	}

	// ILIKE is case-insensitive for ASCII titles.
	ascii := newTodo(t, workspaceID, ownerUserID, "Weekly Report", nil)
	if err := store.Insert(ctx, ascii); err != nil {
		t.Fatalf("Insert(ascii) error = %v", err)
	}
	caseInsensitive, err := store.SearchCandidates(ctx, workspaceID, ownerUserID, "weekly", dto.MaxCandidateLimit)
	if err != nil || len(caseInsensitive) != 1 {
		t.Fatalf("SearchCandidates(weekly) = %#v, err = %v, want 1", caseInsensitive, err)
	}
}

func TestStoreEnforcesCrossWorkspaceIsolation(t *testing.T) {
	pool := setupTestDB(t)
	ctx := context.Background()
	store := NewStore(pool)
	workspaceA, workspaceB := randomID(t), randomID(t)
	owner := randomID(t)

	todo := newTodo(t, workspaceA, owner, "工作区A的任务", nil)
	if err := store.Insert(ctx, todo); err != nil {
		t.Fatalf("Insert() error = %v", err)
	}

	if _, err := store.Get(ctx, workspaceB, owner, todo.ID); !errors.Is(err, domain.ErrTodoNotFound) {
		t.Fatalf("Get(other workspace) error = %v, want ErrTodoNotFound", err)
	}

	completed := todo
	if err := completed.Complete(1, testNow); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if err := store.Update(ctx, completed, 1); err != nil {
		t.Fatalf("Update(own workspace) error = %v", err)
	}
	intruder, err := store.Get(ctx, workspaceA, owner, todo.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	intruder.WorkspaceID = workspaceB // attempt to move the row across workspaces
	if err := store.Update(ctx, intruder, 2); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("Update(other workspace) error = %v, want ErrConflict", err)
	}

	listed, err := store.List(ctx, workspaceB, owner, dto.ListFilters{}, dto.MaxListLimit)
	if err != nil || len(listed) != 0 {
		t.Fatalf("List(other workspace) = %d, err = %v, want 0", len(listed), err)
	}
	candidates, err := store.SearchCandidates(ctx, workspaceB, owner, "工作区A", dto.MaxCandidateLimit)
	if err != nil || len(candidates) != 0 {
		t.Fatalf("SearchCandidates(other workspace) = %d, err = %v, want 0", len(candidates), err)
	}
	summary, err := store.Dashboard(ctx, workspaceB, owner, testNow, testNow, testNow.Add(24*time.Hour))
	if err != nil || summary.PendingTotal != 0 {
		t.Fatalf("Dashboard(other workspace) = %#v, err = %v, want zero counters", summary, err)
	}
}

func ptrTime(v time.Time) *time.Time { return &v }

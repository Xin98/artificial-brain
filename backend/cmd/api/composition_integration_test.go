package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/noopjob"
	reminderports "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/Xin98/artificial-brain/backend/internal/platform/systemhealth"
	"github.com/Xin98/artificial-brain/backend/internal/platform/workerstatus"
)

func setupAPIHandler(t *testing.T, devInbox bool) http.Handler {
	t.Helper()
	handler, _ := setupAPIHandlerWithPool(t, devInbox)
	return handler
}

func setupAPIHandlerWithPool(t *testing.T, devInbox bool) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	dbURL, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	directory := filepath.Join("..", "..", "..", "deploy", "migrations")
	if err := database.RunMigrations(ctx, dbURL, directory); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	pool, err := database.OpenPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("OpenPool() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `
		truncate identity.login_challenges, identity.sessions, identity.contact_channels,
			identity.users, identity.workspaces, identity.message_outbox,
			todo.todos, reminder.reminder_plans,
			conversation.confirmation_requests, conversation.messages restart identity cascade
	`); err != nil {
		t.Fatalf("truncate error = %v", err)
	}
	cfg := config.Config{
		AppEnv:            "development",
		DevInboxEnabled:   devInbox,
		SessionTTL:        time.Hour,
		LoginChallengeTTL: 5 * time.Minute,
		ChannelCodeTTL:    10 * time.Minute,
		ConfirmationTTL:   5 * time.Minute,
	}
	ready := func(context.Context) error { return nil }
	checker := systemhealth.NewChecker(pool, workerstatus.NewRegistry(pool, time.Now), time.Now, 6*time.Second)
	return buildHandler(cfg, pool, ready, checker), pool
}

func doJSON(t *testing.T, client *http.Client, method, target, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, target, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestLoginRoundTripAndLogout(t *testing.T) {
	handler := setupAPIHandler(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	client := &http.Client{}
	phone := "+8613900001111"

	resp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/auth/login/request", `{"phone":"`+phone+`"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("login request status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()

	inboxResp, err := client.Get(srv.URL + "/api/v1/dev/sms-inbox?address=" + url.QueryEscape(phone))
	if err != nil {
		t.Fatal(err)
	}
	if inboxResp.StatusCode != http.StatusOK {
		t.Fatalf("dev inbox status = %d, want 200", inboxResp.StatusCode)
	}
	var inbox struct {
		Messages []struct {
			Code string `json:"code"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(inboxResp.Body).Decode(&inbox); err != nil {
		t.Fatal(err)
	}
	inboxResp.Body.Close()
	if len(inbox.Messages) == 0 || len(inbox.Messages[0].Code) != 6 {
		t.Fatalf("inbox messages = %#v", inbox.Messages)
	}
	code := inbox.Messages[0].Code

	verifyResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/auth/login/verify", `{"phone":"`+phone+`","code":"`+code+`"}`)
	if verifyResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(verifyResp.Body)
		t.Fatalf("verify status = %d, want 200, body=%s", verifyResp.StatusCode, body)
	}
	var sessionCookie *http.Cookie
	for _, c := range verifyResp.Cookies() {
		if c.Name == "ab_session" {
			sessionCookie = c
		}
	}
	verifyResp.Body.Close()
	if sessionCookie == nil || sessionCookie.Value == "" {
		t.Fatal("verify did not set ab_session cookie")
	}

	sessionReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/auth/session", nil)
	sessionReq.AddCookie(sessionCookie)
	sessionResp, err := client.Do(sessionReq)
	if err != nil {
		t.Fatal(err)
	}
	if sessionResp.StatusCode != http.StatusOK {
		t.Fatalf("session status = %d, want 200", sessionResp.StatusCode)
	}
	sessionResp.Body.Close()

	logoutReq, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutResp, err := client.Do(logoutReq)
	if err != nil {
		t.Fatal(err)
	}
	if logoutResp.StatusCode != http.StatusOK {
		t.Fatalf("logout status = %d, want 200", logoutResp.StatusCode)
	}
	logoutResp.Body.Close()

	postLogoutReq, _ := http.NewRequest(http.MethodGet, srv.URL+"/api/v1/auth/session", nil)
	postLogoutReq.AddCookie(sessionCookie)
	postLogoutResp, err := client.Do(postLogoutReq)
	if err != nil {
		t.Fatal(err)
	}
	if postLogoutResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("post-logout session status = %d, want 401", postLogoutResp.StatusCode)
	}
	postLogoutResp.Body.Close()
}

func TestDevInboxAbsentWhenDisabled(t *testing.T) {
	handler := setupAPIHandler(t, false)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/api/v1/dev/sms-inbox?address=%2B8613900002222")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled dev inbox status = %d, want 404", resp.StatusCode)
	}
}

func TestHealthRoutesStillServed(t *testing.T) {
	handler := setupAPIHandler(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := srv.Client().Get(srv.URL + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health/live status = %d, want 200", resp.StatusCode)
	}
}

func setupTodoPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL, ok := os.LookupEnv("TEST_DATABASE_URL")
	if !ok {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	directory := filepath.Join("..", "..", "..", "deploy", "migrations")
	if err := database.RunMigrations(ctx, dbURL, directory); err != nil {
		t.Fatalf("RunMigrations() error = %v", err)
	}
	pool, err := database.OpenPool(ctx, dbURL)
	if err != nil {
		t.Fatalf("OpenPool() error = %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, `truncate todo.todos, reminder.reminder_plans cascade`); err != nil {
		t.Fatalf("truncate error = %v", err)
	}
	return pool
}

type failingScheduler struct{ err error }

func (s failingScheduler) Schedule(context.Context, reminderports.ReminderJob) ([]reminderports.ScheduledChannel, error) {
	return nil, s.err
}

func (s failingScheduler) Cancel(context.Context, int64) error { return nil }

type planRow struct {
	version     int
	status      string
	scheduledAt time.Time
}

func plansForTodo(t *testing.T, pool *pgxpool.Pool, todoID string) []planRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		select todo_reminder_version, status, scheduled_at_utc
		from reminder.reminder_plans where todo_id = $1
		order by todo_reminder_version
	`, todoID)
	if err != nil {
		t.Fatalf("query plans error = %v", err)
	}
	defer rows.Close()
	var plans []planRow
	for rows.Next() {
		var row planRow
		if err := rows.Scan(&row.version, &row.status, &row.scheduledAt); err != nil {
			t.Fatalf("scan plan error = %v", err)
		}
		plans = append(plans, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows error = %v", err)
	}
	return plans
}

func countTodosInWorkspace(t *testing.T, pool *pgxpool.Pool, workspaceID string) int {
	t.Helper()
	var count int
	if err := pool.QueryRow(context.Background(),
		`select count(*) from todo.todos where workspace_id = $1`, workspaceID).Scan(&count); err != nil {
		t.Fatalf("count todos error = %v", err)
	}
	return count
}

// TestTodoReminderAtomicComposition is the D1 seam evidence: todo writes and
// reminder plans commit or roll back as one transaction.
func TestTodoReminderAtomicComposition(t *testing.T) {
	pool := setupTodoPool(t)
	ctx := context.Background()
	fixed := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }
	workspaceID, ownerUserID := newID(), newID()
	due := fixed.Add(24 * time.Hour)

	// A failing scheduler rolls back both the todo and the reminder plan.
	failing := buildTodoHandlers(pool, failingScheduler{errors.New("scheduler down")}, now)
	_, err := failing.Create.Handle(ctx, tododto.CreateTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, Title: "原子待办", DueAtUTC: &due,
	})
	if err == nil {
		t.Fatal("Create with failing scheduler error = nil, want failure")
	}
	if count := countTodosInWorkspace(t, pool, workspaceID); count != 0 {
		t.Fatalf("todos after rollback = %d, want 0", count)
	}
	var planCount int
	if err := pool.QueryRow(ctx, `select count(*) from reminder.reminder_plans`).Scan(&planCount); err != nil {
		t.Fatalf("count plans error = %v", err)
	}
	if planCount != 0 {
		t.Fatalf("plans after rollback = %d, want 0", planCount)
	}

	// With the noop scheduler both rows commit and the plan fires at the due.
	handlers := buildTodoHandlers(pool, noopjob.New(), now)
	created, err := handlers.Create.Handle(ctx, tododto.CreateTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, Title: "原子待办", DueAtUTC: &due,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if count := countTodosInWorkspace(t, pool, workspaceID); count != 1 {
		t.Fatalf("todos after commit = %d, want 1", count)
	}
	plans := plansForTodo(t, pool, created.ID)
	if len(plans) != 1 || plans[0].status != "planned" || plans[0].version != 1 {
		t.Fatalf("plans after create = %#v, want one planned at v1", plans)
	}
	if !plans[0].scheduledAt.Equal(due) {
		t.Fatalf("plan scheduled_at = %v, want due %v", plans[0].scheduledAt, due)
	}

	// Completing the todo revokes its plan.
	if _, err := handlers.Complete.Handle(ctx, tododto.CompleteTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, TodoID: created.ID, Version: created.Version,
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	plans = plansForTodo(t, pool, created.ID)
	if len(plans) != 1 || plans[0].status != "revoked" {
		t.Fatalf("plans after complete = %#v, want revoked", plans)
	}

	// Rescheduling revokes the old plan and plans the new due at version 2.
	second, err := handlers.Create.Handle(ctx, tododto.CreateTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, Title: "改期待办", DueAtUTC: &due,
	})
	if err != nil {
		t.Fatalf("Create(second) error = %v", err)
	}
	newDue := fixed.Add(48 * time.Hour)
	updated, err := handlers.Update.Handle(ctx, tododto.UpdateTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, TodoID: second.ID, Version: second.Version,
		DueChanged: true, DueAtUTC: &newDue,
	})
	if err != nil {
		t.Fatalf("Update(reschedule) error = %v", err)
	}
	if updated.ReminderVersion != 2 {
		t.Fatalf("updated.ReminderVersion = %d, want 2", updated.ReminderVersion)
	}
	plans = plansForTodo(t, pool, second.ID)
	if len(plans) != 2 {
		t.Fatalf("plans after reschedule = %#v, want old revoked + new planned", plans)
	}
	if plans[0].version != 1 || plans[0].status != "revoked" {
		t.Fatalf("old plan = %#v, want revoked v1", plans[0])
	}
	if plans[1].version != 2 || plans[1].status != "planned" || !plans[1].scheduledAt.Equal(newDue) {
		t.Fatalf("new plan = %#v, want planned v2 at new due", plans[1])
	}
}

// TestTodoDashboardHttpRoundTrip exercises the wired todo routes end to end
// over cookie authentication: create, list, complete, dashboard.
func TestTodoDashboardHttpRoundTrip(t *testing.T) {
	handler := setupAPIHandler(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar error = %v", err)
	}
	client := &http.Client{Jar: jar}
	phone := "+8613900003333"

	resp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/auth/login/request", `{"phone":"`+phone+`"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("login request status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()

	inboxResp, err := client.Get(srv.URL + "/api/v1/dev/sms-inbox?address=" + url.QueryEscape(phone))
	if err != nil {
		t.Fatal(err)
	}
	var inbox struct {
		Messages []struct {
			Code string `json:"code"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(inboxResp.Body).Decode(&inbox); err != nil {
		t.Fatal(err)
	}
	inboxResp.Body.Close()
	if len(inbox.Messages) == 0 {
		t.Fatal("dev inbox returned no messages")
	}

	verifyResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/auth/login/verify",
		`{"phone":"`+phone+`","code":"`+inbox.Messages[0].Code+`"}`)
	if verifyResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(verifyResp.Body)
		t.Fatalf("verify status = %d, want 200, body=%s", verifyResp.StatusCode, body)
	}
	verifyResp.Body.Close()

	// Unauthenticated clients cannot reach the todo routes.
	unauthenticated := doJSON(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/todos", `{"title":"x"}`)
	if unauthenticated.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated create status = %d, want 401", unauthenticated.StatusCode)
	}
	unauthenticated.Body.Close()

	createResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/todos", `{"title":"冒烟待办"}`)
	if createResp.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(createResp.Body)
		t.Fatalf("create status = %d, want 201, body=%s", createResp.StatusCode, body)
	}
	var created struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	if err := json.NewDecoder(createResp.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	createResp.Body.Close()
	if created.ID == "" || created.Version != 1 {
		t.Fatalf("created = %#v", created)
	}

	listResp, err := client.Get(srv.URL + "/api/v1/todos")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Todos []struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		} `json:"todos"`
	}
	if err := json.NewDecoder(listResp.Body).Decode(&listed); err != nil {
		t.Fatal(err)
	}
	listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK || len(listed.Todos) != 1 || listed.Todos[0].ID != created.ID {
		t.Fatalf("list = %#v (status %d), want the created todo", listed, listResp.StatusCode)
	}

	completeResp := doJSON(t, client, http.MethodPost,
		srv.URL+"/api/v1/todos/"+created.ID+"/complete", fmt.Sprintf(`{"version":%d}`, created.Version))
	if completeResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(completeResp.Body)
		t.Fatalf("complete status = %d, want 200, body=%s", completeResp.StatusCode, body)
	}
	var completed struct {
		Status string `json:"status"`
	}
	if err := json.NewDecoder(completeResp.Body).Decode(&completed); err != nil {
		t.Fatal(err)
	}
	completeResp.Body.Close()
	if completed.Status != "completed" {
		t.Fatalf("completed status = %q", completed.Status)
	}

	dashboardResp, err := client.Get(srv.URL + "/api/v1/dashboard/summary?timezone=UTC")
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		PendingTotal       int `json:"pendingTotal"`
		CompletedLast7Days int `json:"completedLast7Days"`
		ReminderRetrying   int `json:"reminderRetrying"`
		ReminderFailed     int `json:"reminderFailed"`
	}
	if err := json.NewDecoder(dashboardResp.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	dashboardResp.Body.Close()
	if dashboardResp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", dashboardResp.StatusCode)
	}
	if summary.PendingTotal != 0 || summary.CompletedLast7Days != 1 {
		t.Fatalf("summary = %#v, want pending 0 and completed 1", summary)
	}
	if summary.ReminderRetrying != 0 || summary.ReminderFailed != 0 {
		t.Fatalf("reminder counters = %#v, want deterministic zeros", summary)
	}
}

// TestConversationEndToEndIntentPath proves the full deterministic loop over
// cookie authentication: conversation create -> reminder plan, list, delete
// with confirmation, and the audit trail of resolved turns.
func TestConversationEndToEndIntentPath(t *testing.T) {
	handler, pool := setupAPIHandlerWithPool(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx := context.Background()

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar error = %v", err)
	}
	client := &http.Client{Jar: jar}
	phone := "+8613900004444"

	resp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/auth/login/request", `{"phone":"`+phone+`"}`)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("login request status = %d, want 202", resp.StatusCode)
	}
	resp.Body.Close()
	inboxResp, err := client.Get(srv.URL + "/api/v1/dev/sms-inbox?address=" + url.QueryEscape(phone))
	if err != nil {
		t.Fatal(err)
	}
	var inbox struct {
		Messages []struct {
			Code string `json:"code"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(inboxResp.Body).Decode(&inbox); err != nil {
		t.Fatal(err)
	}
	inboxResp.Body.Close()
	if len(inbox.Messages) == 0 {
		t.Fatal("dev inbox returned no messages")
	}
	verifyResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/auth/login/verify",
		`{"phone":"`+phone+`","code":"`+inbox.Messages[0].Code+`"}`)
	if verifyResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(verifyResp.Body)
		t.Fatalf("verify status = %d, want 200, body=%s", verifyResp.StatusCode, body)
	}
	verifyResp.Body.Close()

	sessionResp, err := client.Get(srv.URL + "/api/v1/auth/session")
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		UserID      string `json:"userId"`
		WorkspaceID string `json:"workspaceId"`
	}
	if err := json.NewDecoder(sessionResp.Body).Decode(&session); err != nil {
		t.Fatal(err)
	}
	sessionResp.Body.Close()

	postMessage := func(text, timezone string) map[string]any {
		t.Helper()
		messageResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/conversation/messages",
			`{"text":"`+text+`","timezone":"`+timezone+`"}`)
		body, _ := io.ReadAll(messageResp.Body)
		messageResp.Body.Close()
		if messageResp.StatusCode != http.StatusOK {
			t.Fatalf("messages(%s) status = %d, want 200, body=%s", text, messageResp.StatusCode, body)
		}
		var decoded map[string]any
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("messages(%s) body error = %v", text, err)
		}
		return decoded
	}

	// 1. "明天下午三点提醒我提交周报" creates the todo and its reminder plan.
	created := postMessage("明天下午三点提醒我提交周报", "Asia/Shanghai")
	if created["kind"] != "todo_created" {
		t.Fatalf("create kind = %v, want todo_created", created["kind"])
	}
	todoView, ok := created["todo"].(map[string]any)
	if !ok {
		t.Fatalf("create body missing todo: %#v", created)
	}
	todoID, _ := todoView["id"].(string)
	if todoID == "" {
		t.Fatalf("created todo has no id: %#v", todoView)
	}
	var dueAtUTC time.Time
	if err := pool.QueryRow(ctx, `select due_at_utc from todo.todos where id = $1`, todoID).Scan(&dueAtUTC); err != nil {
		t.Fatalf("todo row error = %v", err)
	}
	var planStatus string
	var scheduledAtUTC time.Time
	if err := pool.QueryRow(ctx,
		`select status, scheduled_at_utc from reminder.reminder_plans where todo_id = $1`, todoID).
		Scan(&planStatus, &scheduledAtUTC); err != nil {
		t.Fatalf("plan row error = %v", err)
	}
	if planStatus != "planned" || !scheduledAtUTC.Equal(dueAtUTC) {
		t.Fatalf("plan = %s at %v, want planned at due %v", planStatus, scheduledAtUTC, dueAtUTC)
	}

	// 2. The list intent sees the created todo.
	listed := postMessage("我有什么待办", "Asia/Shanghai")
	if listed["kind"] != "todo_list" {
		t.Fatalf("list kind = %v, want todo_list", listed["kind"])
	}
	if !todosContain(listed, "提交周报") {
		t.Fatalf("list before delete = %#v, want 提交周报", listed["todos"])
	}

	// 3. The delete intent requires a confirmation.
	deletion := postMessage("删除周报", "Asia/Shanghai")
	if deletion["kind"] != "confirmation_required" {
		t.Fatalf("delete kind = %v, want confirmation_required", deletion["kind"])
	}
	confirmationID, _ := deletion["confirmationId"].(string)
	if confirmationID == "" {
		t.Fatalf("delete body missing confirmationId: %#v", deletion)
	}

	// 4. Confirming deletes the todo and revokes the plan.
	confirmResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/confirmations/"+confirmationID+"/confirm", "")
	confirmBody, _ := io.ReadAll(confirmResp.Body)
	confirmResp.Body.Close()
	if confirmResp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200, body=%s", confirmResp.StatusCode, confirmBody)
	}
	var confirmed map[string]any
	if err := json.Unmarshal(confirmBody, &confirmed); err != nil {
		t.Fatal(err)
	}
	if confirmed["kind"] != "todo_deleted" || confirmed["todoId"] != todoID {
		t.Fatalf("confirm body = %#v", confirmed)
	}
	var todoStatus string
	if err := pool.QueryRow(ctx, `select status from todo.todos where id = $1`, todoID).Scan(&todoStatus); err != nil {
		t.Fatalf("todo status error = %v", err)
	}
	if todoStatus != "deleted" {
		t.Fatalf("todo status = %q, want deleted", todoStatus)
	}
	if err := pool.QueryRow(ctx,
		`select status from reminder.reminder_plans where todo_id = $1`, todoID).Scan(&planStatus); err != nil {
		t.Fatalf("plan status error = %v", err)
	}
	if planStatus != "revoked" {
		t.Fatalf("plan status = %q, want revoked", planStatus)
	}

	// 5. A second confirm fails single-use.
	secondResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/confirmations/"+confirmationID+"/confirm", "")
	secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusConflict {
		t.Fatalf("second confirm status = %d, want 409", secondResp.StatusCode)
	}

	// 6. The list intent no longer sees the deleted todo.
	after := postMessage("我有什么待办", "Asia/Shanghai")
	if todosContain(after, "提交周报") {
		t.Fatalf("list after delete = %#v, want 提交周报 gone", after["todos"])
	}

	// 7. Unmatched text is unsupported.
	unknown := postMessage("今天天气怎么样", "Asia/Shanghai")
	if unknown["kind"] != "unsupported" {
		t.Fatalf("unknown kind = %v, want unsupported", unknown["kind"])
	}

	// 8. The audit trail holds exactly the resolved user turns, in order.
	rows, err := pool.Query(ctx, `
		select resolved_intent from conversation.messages
		where workspace_id = $1 and user_id = $2
		order by id
	`, session.WorkspaceID, session.UserID)
	if err != nil {
		t.Fatalf("messages query error = %v", err)
	}
	defer rows.Close()
	var intents []string
	for rows.Next() {
		var intent *string
		if err := rows.Scan(&intent); err != nil {
			t.Fatalf("scan intent error = %v", err)
		}
		if intent != nil {
			intents = append(intents, *intent)
		}
	}
	wantIntents := []string{"todo.create", "todo.list", "todo.delete", "todo.list"}
	if len(intents) != len(wantIntents) {
		t.Fatalf("audit intents = %#v, want %#v", intents, wantIntents)
	}
	for index := range wantIntents {
		if intents[index] != wantIntents[index] {
			t.Fatalf("audit intents = %#v, want %#v", intents, wantIntents)
		}
	}
}

func todosContain(body map[string]any, title string) bool {
	todos, ok := body["todos"].([]any)
	if !ok {
		return false
	}
	for _, item := range todos {
		view, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if view["title"] == title {
			return true
		}
	}
	return false
}

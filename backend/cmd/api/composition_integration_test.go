package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
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
	riverqueue "github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	portabilitypostgres "github.com/Xin98/artificial-brain/backend/internal/modules/portability/adapters/outbound/postgres"
	"github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/noopjob"
	reminderpostgres "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/postgres"
	riversched "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/adapters/outbound/river"
	reminderports "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/application/ports"
	reminderdomain "github.com/Xin98/artificial-brain/backend/internal/modules/reminder/domain"
	tododto "github.com/Xin98/artificial-brain/backend/internal/modules/todo/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/Xin98/artificial-brain/backend/internal/platform/systemhealth"
	"github.com/Xin98/artificial-brain/backend/internal/platform/workerstatus"
)

// receiptSecret is the shared receipt-webhook key the composition wiring is
// built with; test (d) signs its callback with it.
const receiptSecret = "composition-receipt-secret"

func setupAPIHandler(t *testing.T, devInbox bool) http.Handler {
	t.Helper()
	handler, _ := setupAPIHandlerWithPool(t, devInbox)
	return handler
}

// setupTestPool migrates the test database, opens a pool, and truncates every
// business table — identity, todo, reminder, conversation, portability, and
// the river job queue — so each test starts from a clean instance.
func setupTestPool(t *testing.T) *pgxpool.Pool {
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
			todo.todos, reminder.reminder_plans, reminder.fake_outbox,
			conversation.confirmation_requests, conversation.messages,
			portability.portability_imports, portability.portability_source_records,
			public.instance_meta,
			river_job restart identity cascade
	`); err != nil {
		t.Fatalf("truncate error = %v", err)
	}
	return pool
}

func setupAPIHandlerWithPool(t *testing.T, devInbox bool) (http.Handler, *pgxpool.Pool) {
	t.Helper()
	pool := setupTestPool(t)
	cfg := config.Config{
		AppEnv:                    "development",
		DevInboxEnabled:           devInbox,
		ReminderDevOutboxEnabled:  devInbox,
		ReminderReceiptSecret:     receiptSecret,
		SessionTTL:                time.Hour,
		LoginChallengeTTL:         5 * time.Minute,
		ChannelCodeTTL:            10 * time.Minute,
		ConfirmationTTL:           5 * time.Minute,
		DeploymentMode:            config.DeploymentModeCloud,
		PortabilityMaxBundleBytes: 33554432,
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

// sessionIDs carries the authenticated session's scope.
type sessionIDs struct {
	UserID      string
	WorkspaceID string
}

// loginViaDevInbox logs a fresh user in over the phone login-code flow and
// returns an authenticated cookie client plus the session's workspace and
// user ids.
func loginViaDevInbox(t *testing.T, srv *httptest.Server, phone string) (*http.Client, sessionIDs) {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar error = %v", err)
	}
	client := &http.Client{Jar: jar}

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
	if len(inbox.Messages) == 0 || inbox.Messages[0].Code == "" {
		t.Fatal("dev inbox returned no login code")
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
	if session.UserID == "" || session.WorkspaceID == "" {
		t.Fatalf("session = %#v, want userId and workspaceId", session)
	}
	return client, sessionIDs{UserID: session.UserID, WorkspaceID: session.WorkspaceID}
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

	outboxResp, err := srv.Client().Get(srv.URL + "/api/v1/dev/reminder-outbox?address=user%40example.com")
	if err != nil {
		t.Fatal(err)
	}
	defer outboxResp.Body.Close()
	if outboxResp.StatusCode != http.StatusNotFound {
		t.Fatalf("disabled reminder dev outbox status = %d, want 404", outboxResp.StatusCode)
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
	return setupTestPool(t)
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

	// A failing scheduler rolls back the todo, the reminder plan, and the
	// delivery rows together.
	failing := buildTodoHandlers(pool, failingScheduler{errors.New("scheduler down")},
		bothChannelsProvider(), now)
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
	var deliveryCount int
	if err := pool.QueryRow(ctx, `select count(*) from reminder.reminder_deliveries`).Scan(&deliveryCount); err != nil {
		t.Fatalf("count deliveries error = %v", err)
	}
	if deliveryCount != 0 {
		t.Fatalf("deliveries after rollback = %d, want 0", deliveryCount)
	}

	// With the noop scheduler both rows commit and the plan fires at the due.
	handlers := buildTodoHandlers(pool, noopjob.New(), nil, now)
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

// TestManualDeleteCompletedTodo proves the confirmation-gated manual delete
// accepts completed todos: the UI offers delete for every listed todo, and
// the domain Delete transition excludes only already-deleted rows.
func TestManualDeleteCompletedTodo(t *testing.T) {
	handler, pool := setupAPIHandlerWithPool(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx := context.Background()

	client, _ := loginViaDevInbox(t, srv, "+8613900004445")

	created := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/todos", `{"title":"已完成待删除"}`)
	createBody, _ := io.ReadAll(created.Body)
	created.Body.Close()
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body=%s", created.StatusCode, createBody)
	}
	var todo struct {
		ID      string `json:"id"`
		Version int    `json:"version"`
	}
	if err := json.Unmarshal(createBody, &todo); err != nil {
		t.Fatal(err)
	}

	completeResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/todos/"+todo.ID+"/complete",
		fmt.Sprintf(`{"version":%d}`, todo.Version))
	completeBody, _ := io.ReadAll(completeResp.Body)
	completeResp.Body.Close()
	if completeResp.StatusCode != http.StatusOK {
		t.Fatalf("complete status = %d, want 200, body=%s", completeResp.StatusCode, completeBody)
	}

	// The manual delete confirmation must accept the completed todo.
	confirmationResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/confirmations",
		`{"intent":"todo.delete","todoId":"`+todo.ID+`"}`)
	confirmationBody, _ := io.ReadAll(confirmationResp.Body)
	confirmationResp.Body.Close()
	if confirmationResp.StatusCode != http.StatusCreated {
		t.Fatalf("confirmation status = %d, want 201, body=%s", confirmationResp.StatusCode, confirmationBody)
	}
	var confirmation struct {
		ConfirmationID string `json:"confirmationId"`
	}
	if err := json.Unmarshal(confirmationBody, &confirmation); err != nil {
		t.Fatal(err)
	}

	confirmResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/confirmations/"+confirmation.ConfirmationID+"/confirm", "")
	confirmBody, _ := io.ReadAll(confirmResp.Body)
	confirmResp.Body.Close()
	if confirmResp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200, body=%s", confirmResp.StatusCode, confirmBody)
	}
	var confirmed map[string]any
	if err := json.Unmarshal(confirmBody, &confirmed); err != nil {
		t.Fatal(err)
	}
	if confirmed["kind"] != "todo_deleted" || confirmed["todoId"] != todo.ID {
		t.Fatalf("confirm body = %#v", confirmed)
	}

	var status string
	if err := pool.QueryRow(ctx, `select status from todo.todos where id = $1`, todo.ID).Scan(&status); err != nil {
		t.Fatalf("todo status query error = %v", err)
	}
	if status != "deleted" {
		t.Fatalf("todo status = %q, want deleted", status)
	}

	listResp, err := client.Get(srv.URL + "/api/v1/todos")
	if err != nil {
		t.Fatal(err)
	}
	listBody, _ := io.ReadAll(listResp.Body)
	listResp.Body.Close()
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listResp.StatusCode)
	}
	if strings.Contains(string(listBody), "已完成待删除") {
		t.Fatalf("list after delete still holds the todo: %s", listBody)
	}
}

// bothChannelsProvider snapshots both channel kinds for every owner; it lets
// the composition tests drive the delivery fan-out without identity rows.
func bothChannelsProvider() func(context.Context, string, string) ([]string, error) {
	return func(context.Context, string, string) ([]string, error) {
		return []string{"email", "sms"}, nil
	}
}

// TestRiverSchedulerCommitsJobsAtomically is the River-backed atomicity
// variant of the failing-scheduler proof: the todo, the plan, the delivery
// rows, and the river_job rows all commit together, and the delivery rows
// carry the real provider job IDs.
func TestRiverSchedulerCommitsJobsAtomically(t *testing.T) {
	pool := setupTodoPool(t)
	ctx := context.Background()
	fixed := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }
	due := fixed.Add(24 * time.Hour)

	riverClient, err := riverqueue.NewClient(riverpgxv5.New(pool), &riverqueue.Config{})
	if err != nil {
		t.Fatalf("river NewClient() error = %v", err)
	}
	handlers := buildTodoHandlers(pool, riversched.New(riverClient), bothChannelsProvider(), now)

	workspaceID, ownerUserID := newID(), newID()
	created, err := handlers.Create.Handle(ctx, tododto.CreateTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, Title: "River 原子待办", DueAtUTC: &due,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	plans := plansForTodo(t, pool, created.ID)
	if len(plans) != 1 || plans[0].status != "planned" || plans[0].version != 1 {
		t.Fatalf("plans after create = %#v, want one planned at v1", plans)
	}
	var requested []string
	if err := pool.QueryRow(ctx,
		`select requested_channels from reminder.reminder_plans where todo_id = $1`, created.ID).
		Scan(&requested); err != nil {
		t.Fatalf("requested channels error = %v", err)
	}
	if len(requested) != 2 || requested[0] != "email" || requested[1] != "sms" {
		t.Fatalf("requested_channels = %#v, want {email sms}", requested)
	}

	rows, err := pool.Query(ctx, `
		select channel, provider_job_id
		from reminder.reminder_deliveries
		where todo_id = $1
		order by channel
	`, created.ID)
	if err != nil {
		t.Fatalf("deliveries query error = %v", err)
	}
	defer rows.Close()
	type deliveryRow struct {
		channel string
		jobID   *int64
	}
	var deliveries []deliveryRow
	for rows.Next() {
		var row deliveryRow
		if err := rows.Scan(&row.channel, &row.jobID); err != nil {
			t.Fatalf("scan delivery error = %v", err)
		}
		deliveries = append(deliveries, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("delivery rows error = %v", err)
	}
	if len(deliveries) != 2 {
		t.Fatalf("deliveries = %#v, want one per channel", deliveries)
	}
	for _, delivery := range deliveries {
		if delivery.jobID == nil {
			t.Fatalf("delivery %s provider_job_id = NULL, want a real river job id", delivery.channel)
		}
		var queue, state string
		var scheduledAt time.Time
		if err := pool.QueryRow(ctx,
			`select queue, state, scheduled_at from river_job where id = $1`, *delivery.jobID).
			Scan(&queue, &state, &scheduledAt); err != nil {
			t.Fatalf("river_job %d error = %v", *delivery.jobID, err)
		}
		if queue != "reminder_"+delivery.channel {
			t.Fatalf("river_job %d queue = %q, want reminder_%s", *delivery.jobID, queue, delivery.channel)
		}
		if state != "scheduled" {
			t.Fatalf("river_job %d state = %q, want scheduled", *delivery.jobID, state)
		}
		if !scheduledAt.Equal(due) {
			t.Fatalf("river_job %d scheduled_at = %v, want due %v", *delivery.jobID, scheduledAt, due)
		}
	}
}

// TestChannelsSnapshotPlansBothChannels proves the channels provider seam:
// verified+enabled contact channels registered through the public identity
// handlers land in the plan's requested_channels snapshot and one delivery
// row per channel.
func TestChannelsSnapshotPlansBothChannels(t *testing.T) {
	handler, pool := setupAPIHandlerWithPool(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx := context.Background()

	client, _ := loginViaDevInbox(t, srv, "+8613900005555")

	addChannel := func(kind, address string) string {
		t.Helper()
		resp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/settings/contact-channels",
			`{"kind":"`+kind+`","address":"`+address+`"}`)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("add %s channel status = %d, want 201, body=%s", kind, resp.StatusCode, body)
		}
		var view struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(body, &view); err != nil {
			t.Fatal(err)
		}
		if view.ID == "" {
			t.Fatalf("add %s channel returned no id: %s", kind, body)
		}
		return view.ID
	}
	latestCode := func(address string) string {
		t.Helper()
		resp, err := client.Get(srv.URL + "/api/v1/dev/sms-inbox?address=" + url.QueryEscape(address))
		if err != nil {
			t.Fatal(err)
		}
		var inbox struct {
			Messages []struct {
				Code string `json:"code"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&inbox); err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if len(inbox.Messages) == 0 || inbox.Messages[0].Code == "" {
			t.Fatalf("no verification code in dev inbox for %s", address)
		}
		return inbox.Messages[0].Code
	}
	verifyChannel := func(channelID, code string) {
		t.Helper()
		resp := doJSON(t, client, http.MethodPost,
			srv.URL+"/api/v1/settings/contact-channels/"+channelID+"/verify",
			`{"code":"`+code+`"}`)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("verify channel status = %d, want 200, body=%s", resp.StatusCode, body)
		}
	}

	emailAddress := "owner@example.com"
	smsAddress := "+8613900006666"
	verifyChannel(addChannel("email", emailAddress), latestCode(emailAddress))
	verifyChannel(addChannel("sms", smsAddress), latestCode(smsAddress))

	// The gated reminder dev outbox route is present alongside the dev inbox.
	outboxResp, err := client.Get(srv.URL + "/api/v1/dev/reminder-outbox?address=" + url.QueryEscape(emailAddress))
	if err != nil {
		t.Fatal(err)
	}
	outboxResp.Body.Close()
	if outboxResp.StatusCode != http.StatusOK {
		t.Fatalf("reminder dev outbox status = %d, want 200", outboxResp.StatusCode)
	}

	// A due todo plans both channels: snapshot on the plan, one delivery each.
	createResp := doJSON(t, client, http.MethodPost, srv.URL+"/api/v1/todos",
		`{"title":"渠道待办","dueAtUtc":"2026-08-21T12:00:00Z","timezoneAtInput":"UTC"}`)
	createBody, _ := io.ReadAll(createResp.Body)
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d, want 201, body=%s", createResp.StatusCode, createBody)
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(createBody, &created); err != nil {
		t.Fatal(err)
	}

	var requested []string
	if err := pool.QueryRow(ctx,
		`select requested_channels from reminder.reminder_plans where todo_id = $1`, created.ID).
		Scan(&requested); err != nil {
		t.Fatalf("requested channels error = %v", err)
	}
	if len(requested) != 2 || requested[0] != "email" || requested[1] != "sms" {
		t.Fatalf("requested_channels = %#v, want {email sms}", requested)
	}

	rows, err := pool.Query(ctx, `
		select channel, todo_title_snapshot, provider_job_id is not null
		from reminder.reminder_deliveries
		where todo_id = $1
		order by channel
	`, created.ID)
	if err != nil {
		t.Fatalf("deliveries query error = %v", err)
	}
	defer rows.Close()
	type deliveryRow struct {
		channel  string
		title    string
		hasJobID bool
	}
	var deliveries []deliveryRow
	for rows.Next() {
		var row deliveryRow
		if err := rows.Scan(&row.channel, &row.title, &row.hasJobID); err != nil {
			t.Fatalf("scan delivery error = %v", err)
		}
		deliveries = append(deliveries, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("delivery rows error = %v", err)
	}
	if len(deliveries) != 2 {
		t.Fatalf("deliveries = %#v, want one per channel", deliveries)
	}
	for _, delivery := range deliveries {
		if delivery.title != "渠道待办" {
			t.Fatalf("delivery %s title snapshot = %q, want 渠道待办", delivery.channel, delivery.title)
		}
		if !delivery.hasJobID {
			t.Fatalf("delivery %s provider_job_id = NULL, want a real river job id", delivery.channel)
		}
	}
	if deliveries[0].channel != "email" || deliveries[1].channel != "sms" {
		t.Fatalf("delivery channels = %q,%q, want email,sms", deliveries[0].channel, deliveries[1].channel)
	}
}

// TestDashboardReminderCounters proves the dashboard seam: deliveries seeded
// through the real store (one per lifecycle bucket, including
// sending∧attempt>0) surface as the four reminder counters.
func TestDashboardReminderCounters(t *testing.T) {
	handler, pool := setupAPIHandlerWithPool(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx := context.Background()

	client, session := loginViaDevInbox(t, srv, "+8613900007777")

	plans := reminderpostgres.NewPlanStore(pool)
	deliveries := reminderpostgres.NewDeliveryStore(pool)
	now := time.Now().UTC()
	plan, err := reminderdomain.NewReminderPlan(newID(), session.WorkspaceID, newID(), 1,
		now.Add(time.Hour), []string{"email"}, now)
	if err != nil {
		t.Fatalf("NewReminderPlan() error = %v", err)
	}
	if err := plans.Save(ctx, plan); err != nil {
		t.Fatalf("plans.Save() error = %v", err)
	}

	seed := func(channel string, mutate func(*reminderdomain.ReminderDelivery) error) {
		t.Helper()
		delivery, err := reminderdomain.NewDelivery(newID(), session.WorkspaceID, session.UserID,
			newID(), 1, plan.ID, channel, "仪表盘提醒", now.Add(time.Hour), now)
		if err != nil {
			t.Fatalf("NewDelivery() error = %v", err)
		}
		if mutate != nil {
			if err := mutate(&delivery); err != nil {
				t.Fatalf("mutate delivery error = %v", err)
			}
		}
		if err := deliveries.Save(ctx, delivery); err != nil {
			t.Fatalf("deliveries.Save() error = %v", err)
		}
	}
	seed("email", nil) // scheduled: no dashboard counter
	seed("email", func(d *reminderdomain.ReminderDelivery) error {
		return d.MarkSending(now) // sending ∧ attempt>0 ⇒ retrying
	})
	seed("sms", func(d *reminderdomain.ReminderDelivery) error {
		if err := d.MarkSending(now); err != nil {
			return err
		}
		return d.MarkSucceeded("prov-counters-1", now)
	})
	seed("email", func(d *reminderdomain.ReminderDelivery) error {
		return d.MarkFailed("retry_exhausted", now)
	})
	seed("sms", func(d *reminderdomain.ReminderDelivery) error {
		return d.MarkSuppressed(reminderdomain.ReasonTodoCompleted, now)
	})

	dashboardResp, err := client.Get(srv.URL + "/api/v1/dashboard/summary?timezone=UTC")
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		ReminderSucceeded  int `json:"reminderSucceeded"`
		ReminderRetrying   int `json:"reminderRetrying"`
		ReminderFailed     int `json:"reminderFailed"`
		ReminderSuppressed int `json:"reminderSuppressed"`
	}
	if err := json.NewDecoder(dashboardResp.Body).Decode(&summary); err != nil {
		t.Fatal(err)
	}
	dashboardResp.Body.Close()
	if dashboardResp.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", dashboardResp.StatusCode)
	}
	if summary.ReminderSucceeded != 1 || summary.ReminderRetrying != 1 ||
		summary.ReminderFailed != 1 || summary.ReminderSuppressed != 1 {
		t.Fatalf("reminder counters = %#v, want one per terminal/retrying bucket", summary)
	}
}

// TestReceiptWebhookFlipsReceiptState proves the receipts route is wired: a
// valid HMAC-signed callback flips the delivery's receiptState, and a bad
// signature is rejected.
func TestReceiptWebhookFlipsReceiptState(t *testing.T) {
	handler, pool := setupAPIHandlerWithPool(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx := context.Background()

	_, session := loginViaDevInbox(t, srv, "+8613900008888")

	plans := reminderpostgres.NewPlanStore(pool)
	deliveries := reminderpostgres.NewDeliveryStore(pool)
	now := time.Now().UTC()
	plan, err := reminderdomain.NewReminderPlan(newID(), session.WorkspaceID, newID(), 1,
		now.Add(time.Hour), []string{"sms"}, now)
	if err != nil {
		t.Fatalf("NewReminderPlan() error = %v", err)
	}
	if err := plans.Save(ctx, plan); err != nil {
		t.Fatalf("plans.Save() error = %v", err)
	}
	delivery, err := reminderdomain.NewDelivery(newID(), session.WorkspaceID, session.UserID,
		newID(), 1, plan.ID, "sms", "回执提醒", now.Add(time.Hour), now)
	if err != nil {
		t.Fatalf("NewDelivery() error = %v", err)
	}
	if err := delivery.MarkSending(now); err != nil {
		t.Fatalf("MarkSending() error = %v", err)
	}
	if err := delivery.MarkSucceeded("prov-receipt-1", now); err != nil {
		t.Fatalf("MarkSucceeded() error = %v", err)
	}
	if err := deliveries.Save(ctx, delivery); err != nil {
		t.Fatalf("deliveries.Save() error = %v", err)
	}

	body := `{"providerMessageId":"prov-receipt-1","delivered":true}`
	mac := hmac.New(sha256.New, []byte(receiptSecret))
	mac.Write([]byte(body))
	signature := hex.EncodeToString(mac.Sum(nil))

	post := func(signature string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/webhooks/receipts/sms",
			strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Receipt-Signature", signature)
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	// A bad signature is rejected and leaves the delivery untouched.
	badResp := post("0000")
	badResp.Body.Close()
	if badResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad signature status = %d, want 401", badResp.StatusCode)
	}

	goodResp := post(signature)
	goodBody, _ := io.ReadAll(goodResp.Body)
	goodResp.Body.Close()
	if goodResp.StatusCode != http.StatusOK {
		t.Fatalf("receipt status = %d, want 200, body=%s", goodResp.StatusCode, goodBody)
	}

	var receiptState *string
	if err := pool.QueryRow(ctx,
		`select receipt_state from reminder.reminder_deliveries where provider_message_id = $1`,
		"prov-receipt-1").Scan(&receiptState); err != nil {
		t.Fatalf("receipt_state query error = %v", err)
	}
	if receiptState == nil || *receiptState != "received_ok" {
		t.Fatalf("receipt_state = %v, want received_ok", receiptState)
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

// deliveryFinalizationByTodo returns the state and suppression reason of every
// delivery row for the todo, ordered by channel.
func deliveryFinalizationByTodo(t *testing.T, pool *pgxpool.Pool, todoID string) [][3]string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		select channel, state, coalesce(suppression_reason, '')
		from reminder.reminder_deliveries
		where todo_id = $1
		order by channel
	`, todoID)
	if err != nil {
		t.Fatalf("deliveries query error = %v", err)
	}
	defer rows.Close()
	var result [][3]string
	for rows.Next() {
		var channel, state, reason string
		if err := rows.Scan(&channel, &state, &reason); err != nil {
			t.Fatalf("scan delivery error = %v", err)
		}
		result = append(result, [3]string{channel, state, reason})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("delivery rows error = %v", err)
	}
	return result
}

// TestRevokeSuppressesScheduledDeliveriesOnComplete proves the revoke-time
// finalization: completing a due todo flips its planned delivery rows to
// suppressed with the todo_completed reason inside the caller's transaction,
// atomically with the plan revoke.
func TestRevokeSuppressesScheduledDeliveriesOnComplete(t *testing.T) {
	pool := setupTodoPool(t)
	ctx := context.Background()
	fixed := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }
	due := fixed.Add(24 * time.Hour)

	handlers := buildTodoHandlers(pool, noopjob.New(), bothChannelsProvider(), now)
	workspaceID, ownerUserID := newID(), newID()
	created, err := handlers.Create.Handle(ctx, tododto.CreateTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, Title: "抑制待办", DueAtUTC: &due,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	// Two scheduled delivery rows exist at plan time.
	before := deliveryFinalizationByTodo(t, pool, created.ID)
	if len(before) != 2 {
		t.Fatalf("deliveries before complete = %#v, want one per channel", before)
	}
	for _, row := range before {
		if row[1] != "scheduled" || row[2] != "" {
			t.Fatalf("delivery before complete = %#v, want scheduled without reason", row)
		}
	}

	// Completing the todo suppresses both rows with todo_completed in the same
	// transaction that revokes the plan.
	if _, err := handlers.Complete.Handle(ctx, tododto.CompleteTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, TodoID: created.ID, Version: created.Version,
	}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	after := deliveryFinalizationByTodo(t, pool, created.ID)
	if len(after) != 2 {
		t.Fatalf("deliveries after complete = %#v, want one per channel", after)
	}
	for _, row := range after {
		if row[1] != "suppressed" || row[2] != "todo_completed" {
			t.Fatalf("delivery after complete = %#v, want suppressed(todo_completed)", row)
		}
	}
	plans := plansForTodo(t, pool, created.ID)
	if len(plans) != 1 || plans[0].status != "revoked" {
		t.Fatalf("plans after complete = %#v, want revoked", plans)
	}
}

// TestRevokeSuppressesScheduledDeliveriesOnDelete proves the delete reason
// reaches the delivery rows through the same revoke seam.
func TestRevokeSuppressesScheduledDeliveriesOnDelete(t *testing.T) {
	pool := setupTodoPool(t)
	ctx := context.Background()
	fixed := time.Date(2026, 8, 20, 13, 0, 0, 0, time.UTC)
	now := func() time.Time { return fixed }
	due := fixed.Add(24 * time.Hour)

	handlers := buildTodoHandlers(pool, noopjob.New(), bothChannelsProvider(), now)
	workspaceID, ownerUserID := newID(), newID()
	created, err := handlers.Create.Handle(ctx, tododto.CreateTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, Title: "删除待办", DueAtUTC: &due,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if _, err := handlers.Delete.Handle(ctx, tododto.DeleteTodoRequest{
		WorkspaceID: workspaceID, UserID: ownerUserID, TodoID: created.ID, Version: created.Version,
	}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	after := deliveryFinalizationByTodo(t, pool, created.ID)
	if len(after) != 2 {
		t.Fatalf("deliveries after delete = %#v, want one per channel", after)
	}
	for _, row := range after {
		if row[1] != "suppressed" || row[2] != "todo_deleted" {
			t.Fatalf("delivery after delete = %#v, want suppressed(todo_deleted)", row)
		}
	}
}

// TestProvisionInstanceIdentityIsStable proves the instance identity
// provisioning seam: the portability MetaStore get-or-creates exactly one
// stable instance id, unchanged across repeat provisioning calls.
func TestProvisionInstanceIdentityIsStable(t *testing.T) {
	pool := setupTodoPool(t)
	ctx := context.Background()

	if err := provisionInstanceIdentity(ctx, pool); err != nil {
		t.Fatalf("provisionInstanceIdentity() error = %v", err)
	}
	meta := portabilitypostgres.NewMetaStore(pool)
	first, err := meta.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID() error = %v", err)
	}
	second, err := meta.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID(second) error = %v", err)
	}
	if len(first) != 36 {
		t.Fatalf("instance id = %q, want a 36-char uuid", first)
	}
	if first != second {
		t.Fatalf("instance ids = %q vs %q, want one stable id", first, second)
	}
	// Repeat provisioning observes the same id and never errors.
	if err := provisionInstanceIdentity(ctx, pool); err != nil {
		t.Fatalf("provisionInstanceIdentity(second) error = %v", err)
	}
	third, err := meta.InstanceID(ctx)
	if err != nil {
		t.Fatalf("InstanceID(third) error = %v", err)
	}
	if third != first {
		t.Fatalf("instance id after reprovision = %q, want stable %q", third, first)
	}
}

// identityCounts returns the number of identity users and workspaces.
func identityCounts(t *testing.T, pool *pgxpool.Pool) (int, int) {
	t.Helper()
	var users, workspaces int
	if err := pool.QueryRow(context.Background(), `select count(*) from identity.users`).Scan(&users); err != nil {
		t.Fatalf("count users error = %v", err)
	}
	if err := pool.QueryRow(context.Background(), `select count(*) from identity.workspaces`).Scan(&workspaces); err != nil {
		t.Fatalf("count workspaces error = %v", err)
	}
	return users, workspaces
}

// TestPrivateModeProvisioningAndLoginGate proves the private-deployment
// composition: provisionPrivateAdmin creates the admin workspace+user once
// (a second call is a no-op, cloud mode never provisions), the admin phone
// logs in through the real handler chain with the challenge landing in the
// fake outbox, and any other phone is rejected by the registration-closed
// gate without a challenge ever reaching the outbox.
func TestPrivateModeProvisioningAndLoginGate(t *testing.T) {
	pool := setupTestPool(t)
	ctx := context.Background()

	adminPhone := "+8613900009999"
	cfg := config.Config{
		AppEnv:                    "development",
		DevInboxEnabled:           true,
		SessionTTL:                time.Hour,
		LoginChallengeTTL:         5 * time.Minute,
		ChannelCodeTTL:            10 * time.Minute,
		ConfirmationTTL:           5 * time.Minute,
		DeploymentMode:            config.DeploymentModePrivate,
		PrivateAdminPhone:         adminPhone,
		PortabilityMaxBundleBytes: 33554432,
	}
	ready := func(context.Context) error { return nil }
	checker := systemhealth.NewChecker(pool, workerstatus.NewRegistry(pool, time.Now), time.Now, 6*time.Second)
	srv := httptest.NewServer(buildHandler(cfg, pool, ready, checker))
	defer srv.Close()

	// First provisioning creates the workspace+user once; the second call
	// is a no-op.
	if err := provisionPrivateAdmin(ctx, cfg, pool); err != nil {
		t.Fatalf("provisionPrivateAdmin() error = %v", err)
	}
	if users, workspaces := identityCounts(t, pool); users != 1 || workspaces != 1 {
		t.Fatalf("identity rows after provision = %d users / %d workspaces, want 1/1", users, workspaces)
	}
	if err := provisionPrivateAdmin(ctx, cfg, pool); err != nil {
		t.Fatalf("provisionPrivateAdmin(second) error = %v", err)
	}
	if users, workspaces := identityCounts(t, pool); users != 1 || workspaces != 1 {
		t.Fatalf("identity rows after reprovision = %d users / %d workspaces, want 1/1", users, workspaces)
	}
	var phone string
	if err := pool.QueryRow(ctx, `select phone from identity.users`).Scan(&phone); err != nil {
		t.Fatalf("admin phone query error = %v", err)
	}
	if phone != adminPhone {
		t.Fatalf("provisioned phone = %q, want %q", phone, adminPhone)
	}

	// Cloud mode never provisions.
	cloudCfg := cfg
	cloudCfg.DeploymentMode = config.DeploymentModeCloud
	cloudCfg.PrivateAdminPhone = ""
	if err := provisionPrivateAdmin(ctx, cloudCfg, pool); err != nil {
		t.Fatalf("provisionPrivateAdmin(cloud) error = %v", err)
	}
	if users, workspaces := identityCounts(t, pool); users != 1 || workspaces != 1 {
		t.Fatalf("identity rows after cloud no-op = %d users / %d workspaces, want 1/1", users, workspaces)
	}

	// The admin phone proceeds through the real handler chain: the challenge
	// code lands in the fake outbox and verify issues a session that reuses
	// the provisioned identity instead of registering a new one.
	_, session := loginViaDevInbox(t, srv, adminPhone)
	if users, workspaces := identityCounts(t, pool); users != 1 || workspaces != 1 {
		t.Fatalf("identity rows after admin login = %d users / %d workspaces, want 1/1", users, workspaces)
	}
	var adminUserID, adminWorkspaceID string
	if err := pool.QueryRow(ctx,
		`select id, workspace_id from identity.users where phone = $1`, adminPhone).
		Scan(&adminUserID, &adminWorkspaceID); err != nil {
		t.Fatalf("admin user query error = %v", err)
	}
	if session.UserID != adminUserID || session.WorkspaceID != adminWorkspaceID {
		t.Fatalf("admin session = %#v, want the provisioned identity %s/%s",
			session, adminWorkspaceID, adminUserID)
	}

	// A different phone is rejected by the registration-closed gate: the wired
	// RequestLoginChallengeHandler answers ErrRegistrationClosed, which the
	// identity http layer maps to HTTP 403 + {"code":"registration_closed"}
	// (Global Constraint 4), and no challenge lands in the fake outbox.
	otherPhone := "+8613900001234"
	rejected := doJSON(t, srv.Client(), http.MethodPost, srv.URL+"/api/v1/auth/login/request",
		`{"phone":"`+otherPhone+`"}`)
	rejectedBody, _ := io.ReadAll(rejected.Body)
	rejected.Body.Close()
	if rejected.StatusCode != http.StatusForbidden {
		t.Fatalf("other phone login request status = %d, want 403, body=%s", rejected.StatusCode, rejectedBody)
	}
	var rejectedEnvelope struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rejectedBody, &rejectedEnvelope); err != nil {
		t.Fatalf("rejected body error = %v, body=%s", err, rejectedBody)
	}
	if rejectedEnvelope.Code != "registration_closed" {
		t.Fatalf("rejected code = %q, want registration_closed", rejectedEnvelope.Code)
	}
	inboxResp, err := srv.Client().Get(srv.URL + "/api/v1/dev/sms-inbox?address=" + url.QueryEscape(otherPhone))
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
	if len(inbox.Messages) != 0 {
		t.Fatalf("other phone outbox messages = %#v, want none behind the gate", inbox.Messages)
	}
}

// TestPortabilityRoundTripBetweenTwoWorkspaces proves the full portability
// loop in cloud mode through the real handler chain and two sessions (two
// phone numbers ⇒ two auto-provisioned workspaces): workspace A creates one
// todo with a due date — which plans a reminder — and one channel, exports
// its history into a buffer, workspace B uploads and confirms the bundle,
// and B then holds the copied todo WITHOUT a reminder plan and the channel
// unverified; a second confirm reports the import conflict.
func TestPortabilityRoundTripBetweenTwoWorkspaces(t *testing.T) {
	handler, pool := setupAPIHandlerWithPool(t, true)
	srv := httptest.NewServer(handler)
	defer srv.Close()
	ctx := context.Background()

	clientA, sessionA := loginViaDevInbox(t, srv, "+8613900010001")
	clientB, sessionB := loginViaDevInbox(t, srv, "+8613900010002")
	if sessionA.WorkspaceID == sessionB.WorkspaceID {
		t.Fatalf("both sessions share workspace %s, want two workspaces", sessionA.WorkspaceID)
	}

	// A: verify an email channel first so the due todo plans it.
	addResp := doJSON(t, clientA, http.MethodPost, srv.URL+"/api/v1/settings/contact-channels",
		`{"kind":"email","address":"migrate-a@example.com"}`)
	addBody, _ := io.ReadAll(addResp.Body)
	addResp.Body.Close()
	if addResp.StatusCode != http.StatusCreated {
		t.Fatalf("add channel status = %d, want 201, body=%s", addResp.StatusCode, addBody)
	}
	var channelView struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(addBody, &channelView); err != nil {
		t.Fatal(err)
	}
	codeResp, err := clientA.Get(srv.URL + "/api/v1/dev/sms-inbox?address=" + url.QueryEscape("migrate-a@example.com"))
	if err != nil {
		t.Fatal(err)
	}
	var codeInbox struct {
		Messages []struct {
			Code string `json:"code"`
		} `json:"messages"`
	}
	if err := json.NewDecoder(codeResp.Body).Decode(&codeInbox); err != nil {
		t.Fatal(err)
	}
	codeResp.Body.Close()
	if len(codeInbox.Messages) == 0 || codeInbox.Messages[0].Code == "" {
		t.Fatal("no channel verification code in the fake outbox")
	}
	verifyChannelResp := doJSON(t, clientA, http.MethodPost,
		srv.URL+"/api/v1/settings/contact-channels/"+channelView.ID+"/verify",
		`{"code":"`+codeInbox.Messages[0].Code+`"}`)
	verifyChannelResp.Body.Close()
	if verifyChannelResp.StatusCode != http.StatusOK {
		t.Fatalf("verify channel status = %d, want 200", verifyChannelResp.StatusCode)
	}

	// A: a due todo plans a reminder through the real chain.
	createResp := doJSON(t, clientA, http.MethodPost, srv.URL+"/api/v1/todos",
		`{"title":"迁移待办","dueAtUtc":"2026-08-25T12:00:00Z","timezoneAtInput":"UTC"}`)
	createBody, _ := io.ReadAll(createResp.Body)
	createResp.Body.Close()
	if createResp.StatusCode != http.StatusCreated {
		t.Fatalf("create todo status = %d, want 201, body=%s", createResp.StatusCode, createBody)
	}
	var planCount int
	if err := pool.QueryRow(ctx,
		`select count(*) from reminder.reminder_plans where workspace_id = $1`, sessionA.WorkspaceID).
		Scan(&planCount); err != nil {
		t.Fatalf("count A plans error = %v", err)
	}
	if planCount != 1 {
		t.Fatalf("A plans before export = %d, want the due todo planned once", planCount)
	}

	// A exports its history into a buffer through the real export route.
	exportResp := doJSON(t, clientA, http.MethodPost, srv.URL+"/api/v1/portability/export", `{}`)
	if exportResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(exportResp.Body)
		t.Fatalf("export status = %d, want 200, body=%s", exportResp.StatusCode, body)
	}
	if contentType := exportResp.Header.Get("Content-Type"); contentType != "application/zip" {
		t.Fatalf("export content type = %q, want application/zip", contentType)
	}
	bundle, err := io.ReadAll(exportResp.Body)
	exportResp.Body.Close()
	if err != nil {
		t.Fatalf("export body error = %v", err)
	}
	if len(bundle) == 0 {
		t.Fatal("export bundle is empty")
	}

	// B uploads the bundle: the preview sees all three records as new.
	var uploadBody bytes.Buffer
	writer := multipart.NewWriter(&uploadBody)
	part, err := writer.CreateFormFile("bundle", "export.zip")
	if err != nil {
		t.Fatalf("multipart CreateFormFile error = %v", err)
	}
	if _, err := part.Write(bundle); err != nil {
		t.Fatalf("multipart write error = %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("multipart close error = %v", err)
	}
	uploadReq, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/portability/imports", &uploadBody)
	if err != nil {
		t.Fatal(err)
	}
	uploadReq.Header.Set("Content-Type", writer.FormDataContentType())
	uploadResp, err := clientB.Do(uploadReq)
	if err != nil {
		t.Fatal(err)
	}
	uploadRespBody, _ := io.ReadAll(uploadResp.Body)
	uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated {
		t.Fatalf("upload status = %d, want 201, body=%s", uploadResp.StatusCode, uploadRespBody)
	}
	var uploaded struct {
		ImportID string `json:"importId"`
		Preview  struct {
			New     int `json:"new"`
			Skipped int `json:"skipped"`
		} `json:"preview"`
	}
	if err := json.Unmarshal(uploadRespBody, &uploaded); err != nil {
		t.Fatalf("upload body error = %v", err)
	}
	if uploaded.ImportID == "" {
		t.Fatalf("upload returned no importId: %s", uploadRespBody)
	}
	if uploaded.Preview.New != 3 || uploaded.Preview.Skipped != 0 {
		t.Fatalf("upload preview = %#v, want 3 new records", uploaded.Preview)
	}

	// B confirms: all three records land in exactly one transaction.
	confirmResp := doJSON(t, clientB, http.MethodPost,
		srv.URL+"/api/v1/portability/imports/"+uploaded.ImportID+"/confirm", "")
	confirmBody, _ := io.ReadAll(confirmResp.Body)
	confirmResp.Body.Close()
	if confirmResp.StatusCode != http.StatusOK {
		t.Fatalf("confirm status = %d, want 200, body=%s", confirmResp.StatusCode, confirmBody)
	}
	var report struct {
		New     int `json:"new"`
		Skipped int `json:"skipped"`
	}
	if err := json.Unmarshal(confirmBody, &report); err != nil {
		t.Fatalf("confirm body error = %v", err)
	}
	if report.New != 3 || report.Skipped != 0 {
		t.Fatalf("confirm report = %#v, want 3 new records", report)
	}

	// B holds the copied todo — pending, due preserved, and WITHOUT a
	// reminder plan (import never plans, D7).
	var todoCount int
	if err := pool.QueryRow(ctx,
		`select count(*) from todo.todos where workspace_id = $1`, sessionB.WorkspaceID).
		Scan(&todoCount); err != nil {
		t.Fatalf("count B todos error = %v", err)
	}
	if todoCount != 1 {
		t.Fatalf("B todos after confirm = %d, want the one copied todo", todoCount)
	}
	var title, status string
	var dueAt time.Time
	if err := pool.QueryRow(ctx,
		`select title, status, due_at_utc from todo.todos where workspace_id = $1`, sessionB.WorkspaceID).
		Scan(&title, &status, &dueAt); err != nil {
		t.Fatalf("B todo row error = %v", err)
	}
	if title != "迁移待办" || status != "pending" {
		t.Fatalf("B todo = %q/%q, want 迁移待办/pending", title, status)
	}
	wantDue := time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC)
	if !dueAt.Equal(wantDue) {
		t.Fatalf("B todo due = %v, want %v", dueAt, wantDue)
	}
	var plansInB int
	if err := pool.QueryRow(ctx,
		`select count(*) from reminder.reminder_plans where workspace_id = $1`, sessionB.WorkspaceID).
		Scan(&plansInB); err != nil {
		t.Fatalf("count B plans error = %v", err)
	}
	if plansInB != 0 {
		t.Fatalf("B reminder plans after confirm = %d, want zero — import never plans", plansInB)
	}

	// B holds the copied channel unverified.
	var kind, address string
	var verified bool
	if err := pool.QueryRow(ctx,
		`select kind, address, verified from identity.contact_channels where workspace_id = $1`,
		sessionB.WorkspaceID).Scan(&kind, &address, &verified); err != nil {
		t.Fatalf("B channel row error = %v", err)
	}
	if kind != "email" || address != "migrate-a@example.com" {
		t.Fatalf("B channel = %s/%s, want email/migrate-a@example.com", kind, address)
	}
	if verified {
		t.Fatal("B channel is verified, want imported channels unverified")
	}

	// B holds the copied delivery history as an imported, never-planned row.
	var importedDeliveries int
	if err := pool.QueryRow(ctx,
		`select count(*) from reminder.reminder_deliveries where workspace_id = $1 and origin = 'imported'`,
		sessionB.WorkspaceID).Scan(&importedDeliveries); err != nil {
		t.Fatalf("count B imported deliveries error = %v", err)
	}
	if importedDeliveries != 1 {
		t.Fatalf("B imported deliveries = %d, want the one copied history row", importedDeliveries)
	}

	// The import view renders committed with its report.
	getResp, err := clientB.Get(srv.URL + "/api/v1/portability/imports/" + uploaded.ImportID)
	if err != nil {
		t.Fatal(err)
	}
	getBody, _ := io.ReadAll(getResp.Body)
	getResp.Body.Close()
	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("get import status = %d, want 200, body=%s", getResp.StatusCode, getBody)
	}
	var view struct {
		State string `json:"state"`
	}
	if err := json.Unmarshal(getBody, &view); err != nil {
		t.Fatalf("get import body error = %v", err)
	}
	if view.State != "committed" {
		t.Fatalf("import state = %q, want committed", view.State)
	}

	// A second confirm reports the import conflict.
	secondResp := doJSON(t, clientB, http.MethodPost,
		srv.URL+"/api/v1/portability/imports/"+uploaded.ImportID+"/confirm", "")
	secondBody, _ := io.ReadAll(secondResp.Body)
	secondResp.Body.Close()
	if secondResp.StatusCode != http.StatusConflict {
		t.Fatalf("second confirm status = %d, want 409, body=%s", secondResp.StatusCode, secondBody)
	}
	var conflict struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(secondBody, &conflict); err != nil {
		t.Fatalf("second confirm body error = %v", err)
	}
	if conflict.Code != "import_conflict" {
		t.Fatalf("second confirm code = %q, want import_conflict", conflict.Code)
	}
}

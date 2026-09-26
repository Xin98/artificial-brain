package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

const cannedContent = `{"schemaVersion":"1","intent":"todo.create","arguments":{"title":"提交周报"},"confidence":0.9,"missingFields":[]}`

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type temporaryTimeoutError struct{}

func (temporaryTimeoutError) Error() string   { return "temporary timeout" }
func (temporaryTimeoutError) Timeout() bool   { return true }
func (temporaryTimeoutError) Temporary() bool { return true }

type temporaryTimeoutBody struct{}

func (temporaryTimeoutBody) Read([]byte) (int, error) { return 0, temporaryTimeoutError{} }
func (temporaryTimeoutBody) Close() error             { return nil }

func completionResponse(content string) string {
	return `{"choices":[{"message":{"role":"assistant","content":` + strconv.Quote(content) + `}}]}`
}

func newStartedServer(t *testing.T, handler http.HandlerFunc) (*httptest.Server, Config) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server, Config{
		BaseURL:   server.URL,
		APIKey:    "test-key",
		ModelName: "test-model",
		Timeout:   5 * time.Second,
	}
}

func TestNewRequiresConfiguration(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Fatal("New(empty) error = nil, want configuration error")
	}
	if _, err := New(Config{BaseURL: "http://x", APIKey: "k", ModelName: "m", Timeout: time.Second}); err != nil {
		t.Fatalf("New(valid) error = %v", err)
	}
}

func TestProposeReturnsRawContentAndSendsExpectedRequest(t *testing.T) {
	var gotPath, gotAuth, gotContentType string
	var gotBody []byte
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotContentType = r.Header.Get("Content-Type")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(completionResponse(cannedContent)))
	})
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw, err := adapter.Propose(context.Background(), ports.MessageInput{Text: "明天提醒我提交周报", Timezone: "Asia/Shanghai"})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if strings.TrimSpace(string(raw)) != cannedContent {
		t.Fatalf("raw = %s, want untouched content", raw)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %q, want /chat/completions", gotPath)
	}
	if gotAuth != "Bearer test-key" {
		t.Fatalf("auth = %q, want bearer key", gotAuth)
	}
	if gotContentType != "application/json" {
		t.Fatalf("content-type = %q", gotContentType)
	}
	var body struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("request body error = %v", err)
	}
	if body.Model != "test-model" {
		t.Fatalf("model = %q", body.Model)
	}
	var carriedText, carriedTimezone bool
	for _, message := range body.Messages {
		if strings.Contains(message.Content, "明天提醒我提交周报") {
			carriedText = true
		}
		if strings.Contains(message.Content, "Asia/Shanghai") {
			carriedTimezone = true
		}
	}
	if !carriedText || !carriedTimezone {
		t.Fatalf("messages do not carry the turn: %#v", body.Messages)
	}
}

func TestProposeRequestsStructuredIntentJSONWithCurrentTime(t *testing.T) {
	var gotBody struct {
		ResponseFormat struct {
			Type string `json:"type"`
		} `json:"response_format"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		_, _ = w.Write([]byte(completionResponse(cannedContent)))
	})
	cfg.Now = func() time.Time {
		return time.Date(2026, 9, 26, 3, 45, 0, 0, time.UTC)
	}
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = adapter.Propose(context.Background(), ports.MessageInput{
		Text:     "提醒我提交周报",
		Timezone: "Asia/Shanghai",
	})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if gotBody.ResponseFormat.Type != "json_object" {
		t.Fatalf("response_format.type = %q, want json_object", gotBody.ResponseFormat.Type)
	}
	if len(gotBody.Messages) != 2 {
		t.Fatalf("messages = %#v, want one system message and one user message", gotBody.Messages)
	}
	system := gotBody.Messages[0]
	if system.Role != "system" {
		t.Fatalf("first message role = %q, want system", system.Role)
	}
	for _, required := range []string{
		`"schemaVersion"`, `"arguments"`, `"confidence"`, `"missingFields"`,
		`"todo.create"`, `"todo.delete"`, `"todo.list"`, `"unknown"`,
		`"additionalProperties":false`, "Asia/Shanghai", "2026-09-26T11:45:00+08:00",
	} {
		if !strings.Contains(system.Content, required) {
			t.Errorf("system prompt missing %q: %s", required, system.Content)
		}
	}
	user := gotBody.Messages[1]
	if user.Role != "user" || user.Content != "提醒我提交周报" {
		t.Fatalf("user message = %#v, want the original turn", user)
	}
}

func TestProposePassesThroughUnvalidatedContent(t *testing.T) {
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(completionResponse("not json at all")))
	})
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	raw, err := adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if strings.TrimSpace(string(raw)) != "not json at all" {
		t.Fatalf("raw = %q, want unvalidated passthrough", raw)
	}
}

func TestProposeMapsHTTPFailureToTypedError(t *testing.T) {
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, err = adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"})
	if !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("Propose() error = %v, want ErrRequestFailed", err)
	}
}

func TestProposeRetriesOnceAfterRateLimit(t *testing.T) {
	var attempts atomic.Int32
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			http.Error(w, "provider detail must stay private", http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(completionResponse(cannedContent)))
	})
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	raw, err := adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if string(raw) != cannedContent {
		t.Fatalf("raw = %s, want %s", raw, cannedContent)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestProposeDoesNotRetryOtherClientErrorsOrExposeDetails(t *testing.T) {
	var attempts atomic.Int32
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "private provider response", http.StatusBadRequest)
	})
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"})
	if !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("Propose() error = %v, want ErrRequestFailed", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
	for _, secret := range []string{"private provider response", cfg.APIKey} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed private value %q: %v", secret, err)
		}
	}
}

func TestProposeRetriesServerErrorsAtMostOnce(t *testing.T) {
	var attempts atomic.Int32
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		http.Error(w, "private upstream failure", http.StatusServiceUnavailable)
	})
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"})
	if !errors.Is(err, ErrRequestFailed) {
		t.Fatalf("Propose() error = %v, want ErrRequestFailed", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want exactly 2", got)
	}
	for _, secret := range []string{"private upstream failure", cfg.APIKey} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("error exposed private value %q: %v", secret, err)
		}
	}
}

func TestProposeRejectsMalformedResponses(t *testing.T) {
	cases := map[string]string{
		"non-json body":   "hello",
		"missing choices": `{}`,
		"missing content": `{"choices":[{"message":{"role":"assistant"}}]}`,
		"content not str": `{"choices":[{"message":{"content":123}}]}`,
	}
	for name, body := range cases {
		_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		})
		adapter, err := New(cfg)
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		if _, err := adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"}); !errors.Is(err, ErrMalformedResponse) {
			t.Fatalf("%s: Propose() error = %v, want ErrMalformedResponse", name, err)
		}
	}
}

func TestProposeHonorsContextDeadline(t *testing.T) {
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(300 * time.Millisecond)
		_, _ = w.Write([]byte(completionResponse(cannedContent)))
	})
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := adapter.Propose(ctx, ports.MessageInput{Text: "x", Timezone: "UTC"}); err == nil {
		t.Fatal("Propose() error = nil, want deadline failure")
	}
}

func TestProposeRetriesClientTimeoutWhileParentContextIsActive(t *testing.T) {
	var attempts atomic.Int32
	cfg := Config{
		BaseURL:   "http://model.local",
		APIKey:    "test-key",
		ModelName: "test-model",
		Timeout:   30 * time.Millisecond,
	}
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	adapter.client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		if attempts.Add(1) == 1 {
			return nil, temporaryTimeoutError{}
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(completionResponse(cannedContent))),
			Request:    request,
		}, nil
	})

	raw, err := adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if string(raw) != cannedContent {
		t.Fatalf("raw = %s, want %s", raw, cannedContent)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestProposeDoesNotRetryAfterParentContextDeadline(t *testing.T) {
	var attempts atomic.Int32
	_, cfg := newStartedServer(t, func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		time.Sleep(100 * time.Millisecond)
	})
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	if _, err := adapter.Propose(ctx, ports.MessageInput{Text: "x", Timezone: "UTC"}); err == nil {
		t.Fatal("Propose() error = nil, want deadline failure")
	}
	if got := attempts.Load(); got != 1 {
		t.Fatalf("attempts = %d, want 1", got)
	}
}

func TestProposeRetriesResponseBodyTimeoutWhileParentContextIsActive(t *testing.T) {
	var attempts atomic.Int32
	cfg := Config{
		BaseURL:   "http://model.local",
		APIKey:    "test-key",
		ModelName: "test-model",
		Timeout:   30 * time.Millisecond,
	}
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	adapter.client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		body := io.ReadCloser(temporaryTimeoutBody{})
		if attempts.Add(1) > 1 {
			body = io.NopCloser(strings.NewReader(completionResponse(cannedContent)))
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       body,
			Request:    request,
		}, nil
	})

	raw, err := adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	if string(raw) != cannedContent {
		t.Fatalf("raw = %s, want %s", raw, cannedContent)
	}
	if got := attempts.Load(); got != 2 {
		t.Fatalf("attempts = %d, want 2", got)
	}
}

func TestProposeRetriesWithinOneTotalTimeoutBudget(t *testing.T) {
	cfg := Config{
		BaseURL:   "http://model.local",
		APIKey:    "test-key",
		ModelName: "test-model",
		Timeout:   200 * time.Millisecond,
	}
	adapter, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	started := time.Now()
	var deadlines []time.Time
	adapter.client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		deadline, ok := request.Context().Deadline()
		if !ok {
			t.Fatal("request context has no deadline")
		}
		deadlines = append(deadlines, deadline)
		if len(deadlines) == 1 {
			time.Sleep(60 * time.Millisecond)
			return nil, temporaryTimeoutError{}
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})

	if _, err := adapter.Propose(context.Background(), ports.MessageInput{Text: "x", Timezone: "UTC"}); err == nil {
		t.Fatal("Propose() error = nil, want transport failure")
	}
	elapsed := time.Since(started)
	if len(deadlines) != 2 {
		t.Fatalf("attempts = %d, want 2", len(deadlines))
	}
	latestAllowed := started.Add(cfg.Timeout + 20*time.Millisecond)
	if deadlines[1].After(latestAllowed) {
		t.Errorf("retry deadline = %s, later than total budget deadline %s", deadlines[1], latestAllowed)
	}
	if elapsed > cfg.Timeout+40*time.Millisecond {
		t.Errorf("Propose() elapsed = %s, want at most one %s timeout budget", elapsed, cfg.Timeout)
	}
}

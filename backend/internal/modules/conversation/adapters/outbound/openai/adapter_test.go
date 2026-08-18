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
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

const cannedContent = `{"schemaVersion":"1","intent":"todo.create","arguments":{"title":"提交周报"},"confidence":0.9,"missingFields":[]}`

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

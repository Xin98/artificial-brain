package application

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

func proposal(raw string) json.RawMessage { return json.RawMessage(raw) }

func TestValidateProposalGoldenCreate(t *testing.T) {
	got, err := ValidateProposal(proposal(`{
		"schemaVersion": "1",
		"intent": "todo.create",
		"arguments": {
			"title": "提交周报",
			"description": "周五前",
			"dueAtUtc": "2026-08-19T15:00:00+08:00",
			"timezoneAtInput": "Asia/Shanghai"
		},
		"confidence": 0.95,
		"missingFields": []
	}`))
	if err != nil {
		t.Fatalf("ValidateProposal() error = %v", err)
	}
	if got.Intent != domain.IntentTodoCreate || got.Confidence != 0.95 {
		t.Fatalf("proposal = %#v", got)
	}
	args := got.Arguments
	if args.Title != "提交周报" || args.Description == nil || *args.Description != "周五前" {
		t.Fatalf("arguments = %#v", args)
	}
	wantDue := time.Date(2026, 8, 19, 7, 0, 0, 0, time.UTC)
	if args.DueAtUTC == nil || !args.DueAtUTC.Equal(wantDue) {
		t.Fatalf("due = %v, want %v", args.DueAtUTC, wantDue)
	}
	if args.Timezone != "Asia/Shanghai" {
		t.Fatalf("timezone = %q", args.Timezone)
	}
}

func TestValidateProposalCreateAllowsMissingTitleForClarification(t *testing.T) {
	got, err := ValidateProposal(proposal(`{
		"schemaVersion": "1",
		"intent": "todo.create",
		"arguments": {},
		"confidence": 0.7,
		"missingFields": ["title"]
	}`))
	if err != nil {
		t.Fatalf("ValidateProposal() error = %v", err)
	}
	if len(got.MissingFields) != 1 || got.MissingFields[0] != "title" {
		t.Fatalf("missingFields = %#v", got.MissingFields)
	}
}

func TestValidateProposalGoldenDeleteAndList(t *testing.T) {
	del, err := ValidateProposal(proposal(`{
		"schemaVersion": "1",
		"intent": "todo.delete",
		"arguments": {"keyword": "周报"},
		"confidence": 0.95,
		"missingFields": []
	}`))
	if err != nil {
		t.Fatalf("ValidateProposal(delete) error = %v", err)
	}
	if del.Intent != domain.IntentTodoDelete || del.Arguments.Keyword != "周报" {
		t.Fatalf("delete proposal = %#v", del)
	}

	list, err := ValidateProposal(proposal(`{
		"schemaVersion": "1",
		"intent": "todo.list",
		"arguments": {"keyword": "周报", "status": "pending", "dueFrom": "2026-08-18T00:00:00Z", "dueTo": "2026-08-19T00:00:00Z", "noDue": false},
		"confidence": 0.9,
		"missingFields": []
	}`))
	if err != nil {
		t.Fatalf("ValidateProposal(list) error = %v", err)
	}
	if list.Intent != domain.IntentTodoList || list.Arguments.Keyword != "周报" || list.Arguments.Status != "pending" || list.Arguments.NoDue {
		t.Fatalf("list proposal = %#v", list)
	}
	if list.Arguments.DueFrom == nil || !list.Arguments.DueFrom.Equal(time.Date(2026, 8, 18, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("dueFrom = %v", list.Arguments.DueFrom)
	}
}

func TestValidateProposalAcceptsUnknownIntentForRouter(t *testing.T) {
	got, err := ValidateProposal(proposal(`{
		"schemaVersion": "1",
		"intent": "unknown",
		"arguments": {},
		"confidence": 0.0,
		"missingFields": []
	}`))
	if err != nil {
		t.Fatalf("ValidateProposal(unknown) error = %v", err)
	}
	if got.Intent != domain.IntentUnknown {
		t.Fatalf("intent = %q, want unknown", got.Intent)
	}
}

func TestValidateProposalRejectsSchemaViolations(t *testing.T) {
	cases := map[string]string{
		"unknown top-level field": `{"schemaVersion":"1","intent":"todo.list","arguments":{},"confidence":0.9,"missingFields":[],"bogus":1}`,
		"missing required field":  `{"schemaVersion":"1","intent":"todo.list","arguments":{},"missingFields":[]}`,
		"wrong schemaVersion":     `{"schemaVersion":"2","intent":"todo.list","arguments":{},"confidence":0.9,"missingFields":[]}`,
		"illegal intent enum":     `{"schemaVersion":"1","intent":"todo.destroy","arguments":{},"confidence":0.9,"missingFields":[]}`,
		"confidence above range":  `{"schemaVersion":"1","intent":"todo.list","arguments":{},"confidence":1.2,"missingFields":[]}`,
		"confidence below range":  `{"schemaVersion":"1","intent":"todo.list","arguments":{},"confidence":-0.1,"missingFields":[]}`,
		"confidence wrong type":   `{"schemaVersion":"1","intent":"todo.list","arguments":{},"confidence":"high","missingFields":[]}`,
		"missingFields not array": `{"schemaVersion":"1","intent":"todo.list","arguments":{},"confidence":0.9,"missingFields":"title"}`,
		"arguments not object":    `{"schemaVersion":"1","intent":"todo.list","arguments":[],"confidence":0.9,"missingFields":[]}`,
		"not json":                `{`,
	}
	for name, raw := range cases {
		if _, err := ValidateProposal(proposal(raw)); !errors.Is(err, domain.ErrInvalidProposal) {
			t.Fatalf("%s: error = %v, want ErrInvalidProposal", name, err)
		}
	}
}

func TestValidateProposalRejectsArgumentViolations(t *testing.T) {
	longTitle := strings.Repeat("a", 201)
	longKeyword := strings.Repeat("a", 101)
	cases := map[string]string{
		"title too long":          `{"schemaVersion":"1","intent":"todo.create","arguments":{"title":"` + longTitle + `"},"confidence":0.9,"missingFields":[]}`,
		"empty title":             `{"schemaVersion":"1","intent":"todo.create","arguments":{"title":""},"confidence":0.9,"missingFields":[]}`,
		"malformed dueAtUtc":      `{"schemaVersion":"1","intent":"todo.create","arguments":{"title":"x","dueAtUtc":"not-a-time"},"confidence":0.9,"missingFields":[]}`,
		"dueAtUtc wrong type":     `{"schemaVersion":"1","intent":"todo.create","arguments":{"title":"x","dueAtUtc":123},"confidence":0.9,"missingFields":[]}`,
		"invalid timezone":        `{"schemaVersion":"1","intent":"todo.create","arguments":{"title":"x","timezoneAtInput":"Not/AZone"},"confidence":0.9,"missingFields":[]}`,
		"delete keyword too long": `{"schemaVersion":"1","intent":"todo.delete","arguments":{"keyword":"` + longKeyword + `"},"confidence":0.9,"missingFields":[]}`,
		"delete without keyword":  `{"schemaVersion":"1","intent":"todo.delete","arguments":{},"confidence":0.9,"missingFields":[]}`,
		"list bad status enum":    `{"schemaVersion":"1","intent":"todo.list","arguments":{"status":"deleted"},"confidence":0.9,"missingFields":[]}`,
		"list malformed dueFrom":  `{"schemaVersion":"1","intent":"todo.list","arguments":{"dueFrom":"soon"},"confidence":0.9,"missingFields":[]}`,
		"extra argument key":      `{"schemaVersion":"1","intent":"todo.create","arguments":{"title":"x","bogus":1},"confidence":0.9,"missingFields":[]}`,
		"extra list argument key": `{"schemaVersion":"1","intent":"todo.list","arguments":{"keyword":"x","hijack":true},"confidence":0.9,"missingFields":[]}`,
	}
	for name, raw := range cases {
		if _, err := ValidateProposal(proposal(raw)); !errors.Is(err, domain.ErrInvalidProposal) {
			t.Fatalf("%s: error = %v, want ErrInvalidProposal", name, err)
		}
	}
}

func TestRouterSupportsOnlyRegisteredIntents(t *testing.T) {
	router := NewRouter()
	for _, intent := range []domain.Intent{domain.IntentTodoCreate, domain.IntentTodoDelete, domain.IntentTodoList} {
		if !router.Supports(intent) {
			t.Fatalf("Supports(%q) = false, want registered", intent)
		}
	}
	if router.Supports(domain.IntentUnknown) {
		t.Fatal("Supports(unknown) = true, want unregistered")
	}
	if router.Supports(domain.Intent("todo.destroy")) {
		t.Fatal("Supports(todo.destroy) = true, want unregistered")
	}
}

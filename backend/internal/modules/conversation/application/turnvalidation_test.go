package application

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/domain"
)

func TestValidateModelTurnAcceptsChatOnlyEnvelope(t *testing.T) {
	turn, err := ValidateModelTurn(json.RawMessage(
		`{"schemaVersion":"1","reply":"你好，我可以帮你管理待办。","proposal":null}`))
	if err != nil {
		t.Fatalf("ValidateModelTurn() error = %v", err)
	}
	if turn.Reply != "你好，我可以帮你管理待办。" {
		t.Fatalf("turn.Reply = %q", turn.Reply)
	}
	if turn.Proposal != nil {
		t.Fatalf("turn.Proposal = %#v, want nil for a chat-only turn", turn.Proposal)
	}
}

func TestValidateModelTurnAcceptsNestedV1Proposal(t *testing.T) {
	turn, err := ValidateModelTurn(json.RawMessage(
		`{"schemaVersion":"1","reply":"好的，这就为你查询待办。","proposal":{"schemaVersion":"1","intent":"todo.list","arguments":{},"confidence":0.95,"missingFields":[]}}`))
	if err != nil {
		t.Fatalf("ValidateModelTurn() error = %v", err)
	}
	if turn.Proposal == nil || turn.Proposal.Intent != domain.IntentTodoList || turn.Proposal.Confidence != 0.95 {
		t.Fatalf("turn.Proposal = %#v", turn.Proposal)
	}
}

func TestValidateModelTurnRejectsMalformedEnvelopes(t *testing.T) {
	longReply := strings.Repeat("长", MaxReplyRunes+1)
	cases := map[string]string{
		"malformed JSON":        `{`,
		"array envelope":        `[]`,
		"missing schemaVersion": `{"reply":"好","proposal":null}`,
		"missing reply":         `{"schemaVersion":"1","proposal":null}`,
		"missing proposal":      `{"schemaVersion":"1","reply":"好"}`,
		"unknown extra key":     `{"schemaVersion":"1","reply":"好","proposal":null,"extra":1}`,
		"wrong version":         `{"schemaVersion":"2","reply":"好","proposal":null}`,
		"non-string version":    `{"schemaVersion":1,"reply":"好","proposal":null}`,
		"non-string reply":      `{"schemaVersion":"1","reply":42,"proposal":null}`,
		"empty reply":           `{"schemaVersion":"1","reply":"","proposal":null}`,
		"blank reply":           `{"schemaVersion":"1","reply":"  \n ","proposal":null}`,
		"oversized reply":       `{"schemaVersion":"1","reply":"` + longReply + `","proposal":null}`,
		// The nested proposal must pass the untouched v1 validator: this one
		// misses its required keys and must fail closed at the envelope.
		"invalid nested proposal": `{"schemaVersion":"1","reply":"好","proposal":{"intent":"todo.list"}}`,
		"non-object proposal":     `{"schemaVersion":"1","reply":"好","proposal":"todo.list"}`,
	}
	for name, raw := range cases {
		if _, err := ValidateModelTurn(json.RawMessage(raw)); !errors.Is(err, domain.ErrInvalidModelTurn) {
			t.Fatalf("ValidateModelTurn(%s) error = %v, want ErrInvalidModelTurn", name, err)
		}
	}
}

func TestValidateModelTurnKeepsProposalValidationAsInnerChokePoint(t *testing.T) {
	// A v1 proposal that ValidateProposal rejects (delete without keyword)
	// must surface as an invalid turn, never as a dispatchable proposal.
	_, proposalErr := ValidateProposal(json.RawMessage(
		`{"schemaVersion":"1","intent":"todo.delete","arguments":{},"confidence":0.95,"missingFields":[]}`))
	if !errors.Is(proposalErr, domain.ErrInvalidProposal) {
		t.Fatalf("ValidateProposal() error = %v, want ErrInvalidProposal (v1 pin)", proposalErr)
	}
	_, err := ValidateModelTurn(json.RawMessage(
		`{"schemaVersion":"1","reply":"好","proposal":{"schemaVersion":"1","intent":"todo.delete","arguments":{},"confidence":0.95,"missingFields":[]}}`))
	if !errors.Is(err, domain.ErrInvalidModelTurn) {
		t.Fatalf("ValidateModelTurn() error = %v, want ErrInvalidModelTurn", err)
	}
}

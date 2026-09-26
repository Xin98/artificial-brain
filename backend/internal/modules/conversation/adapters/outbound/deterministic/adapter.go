// Package deterministic is the default ModelPort for development and CI: an
// embedded zh+en corpus producing byte-identical turn envelopes for
// identical input, so the conversation loop is fully testable without a
// real model. Multi-turn history is deliberately ignored — corpus matching
// stays exact regardless of prior turns.
package deterministic

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

// EchoReplyTemplate is the fixed, predictable free-chat reply for input the
// corpus does not match. %s carries the first EchoReplyInputRunes runes of
// the trimmed user text, keeping dev/CI output assertable.
const EchoReplyTemplate = "你说的是：「%s」。这个我暂时不能直接执行。我可以帮你创建、查询或删除待办，也可以继续聊天。"

// EchoReplyInputRunes bounds how much of the user text the echo quotes.
const EchoReplyInputRunes = 100

// Adapter implements ports.ModelPort against the embedded corpus.
type Adapter struct {
	now   func() time.Time
	lines map[string]corpusBuild
}

var _ ports.ModelPort = (*Adapter)(nil)

// New builds the corpus index. The injected clock keeps resolved instants
// deterministic in tests.
func New(now func() time.Time) *Adapter {
	adapter := &Adapter{now: now, lines: make(map[string]corpusBuild, len(corpusLines))}
	for _, line := range corpusLines {
		adapter.lines[normalize(line.text)] = line.build
	}
	return adapter
}

// Complete resolves the turn against the corpus and wraps the proposal in
// the unified envelope with a fixed per-family reply; unmatched text yields
// the unknown proposal plus the echo reply.
func (a *Adapter) Complete(ctx context.Context, in ports.MessageInput) (json.RawMessage, error) {
	location := time.UTC
	if loaded, err := time.LoadLocation(in.Timezone); err == nil && loaded != nil {
		location = loaded
	}
	build, matched := a.lines[normalize(in.Text)]
	var proposal proposalEnvelope
	if matched {
		proposal = build(a.now(), location, in.Timezone)
	} else {
		proposal = unknown()(a.now(), location, in.Timezone)
	}
	if proposal.MissingFields == nil {
		proposal.MissingFields = []string{}
	}
	return json.Marshal(turnEnvelope{
		SchemaVersion: "1",
		Reply:         fixedReply(proposal, in.Text),
		Proposal:      proposal,
	})
}

// fixedReply derives the deterministic natural-language reply from the
// resolved proposal family.
func fixedReply(proposal proposalEnvelope, text string) string {
	switch proposal.Intent {
	case "todo.create":
		if len(proposal.MissingFields) > 0 {
			return fmt.Sprintf("好的，请问「%s」要在什么时间提醒？", proposal.Arguments.Title)
		}
		return fmt.Sprintf("好的，我记下了「%s」的提醒安排。", proposal.Arguments.Title)
	case "todo.delete":
		return fmt.Sprintf("好的，我先找一下与「%s」相关的待办。", proposal.Arguments.Keyword)
	case "todo.list":
		return "好的，这就为你查询待办。"
	default:
		return fmt.Sprintf(EchoReplyTemplate, echoQuote(text))
	}
}

// echoQuote trims and truncates the user text to EchoReplyInputRunes runes.
func echoQuote(text string) string {
	trimmed := strings.TrimSpace(text)
	runes := []rune(trimmed)
	if len(runes) > EchoReplyInputRunes {
		return string(runes[:EchoReplyInputRunes])
	}
	return trimmed
}

func normalize(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

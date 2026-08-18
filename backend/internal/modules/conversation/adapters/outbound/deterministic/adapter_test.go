package deterministic

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

func TestUnmatchedTextProposesUnknown(t *testing.T) {
	got := proposeText(t, "完全没有见过的话", "UTC")
	if got.Intent != "unknown" || got.Confidence != 0.0 {
		t.Fatalf("unknown proposal = %#v", got)
	}
	if got.SchemaVersion != "1" {
		t.Fatalf("schemaVersion = %q, want \"1\"", got.SchemaVersion)
	}
}

func TestMatchingNormalizesWhitespaceAndEnglishCase(t *testing.T) {
	cases := []string{
		"  List My Todos  ",
		"LIST MY TODOS",
		"\tlist my todos\n",
	}
	for _, text := range cases {
		got := proposeText(t, text, "UTC")
		if got.Intent != "todo.list" || got.Confidence != 0.95 {
			t.Fatalf("%q = %q/%v, want todo.list/0.95", text, got.Intent, got.Confidence)
		}
	}

	zh := proposeText(t, "  删除周报 ", "UTC")
	if zh.Intent != "todo.delete" || zh.Arguments.Keyword != "周报" {
		t.Fatalf("padded zh delete = %#v", zh)
	}
}

func TestProposeIsByteIdenticalAcrossCalls(t *testing.T) {
	adapter := New(func() time.Time { return corpusNow })
	input := ports.MessageInput{Text: "明天下午三点提醒我提交周报", Timezone: "Asia/Shanghai"}
	first, err := adapter.Propose(context.Background(), input)
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	for index := 0; index < 5; index++ {
		again, err := adapter.Propose(context.Background(), input)
		if err != nil {
			t.Fatalf("Propose() repeat error = %v", err)
		}
		if !bytes.Equal(first, again) {
			t.Fatalf("repeat %d differs: %s vs %s", index, first, again)
		}
	}
}

func TestInvalidTimezoneFallsBackDeterministically(t *testing.T) {
	adapter := New(func() time.Time { return corpusNow })
	raw, err := adapter.Propose(context.Background(), ports.MessageInput{
		Text: "明天下午三点提醒我提交周报", Timezone: "Not/AZone",
	})
	if err != nil {
		t.Fatalf("Propose() error = %v", err)
	}
	var proposal evalProposal
	if err := json.Unmarshal(raw, &proposal); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	// The due resolves against UTC when the input timezone cannot load; the
	// raw timezone still travels so the application validation rejects it.
	if proposal.Arguments.DueAtUTC != "2026-08-19T15:00:00Z" {
		t.Fatalf("dueAtUtc = %q, want UTC-resolved 2026-08-19T15:00:00Z", proposal.Arguments.DueAtUTC)
	}
	if proposal.Arguments.TimezoneAtInput != "Not/AZone" {
		t.Fatalf("timezoneAtInput = %q, want raw input", proposal.Arguments.TimezoneAtInput)
	}
}

// Package deterministic is the ITER-0002 default ModelPort: an embedded
// zh+en corpus producing byte-identical proposals for identical input, so
// the conversation loop is fully testable without a real model.
package deterministic

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/conversation/application/ports"
)

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

// Propose resolves the turn against the corpus; unmatched text yields the
// unknown proposal with confidence 0.
func (a *Adapter) Propose(ctx context.Context, in ports.MessageInput) (json.RawMessage, error) {
	location := time.UTC
	if loaded, err := time.LoadLocation(in.Timezone); err == nil && loaded != nil {
		location = loaded
	}
	build, matched := a.lines[normalize(in.Text)]
	var envelope proposalEnvelope
	if matched {
		envelope = build(a.now(), location, in.Timezone)
	} else {
		envelope = unknown()(a.now(), location, in.Timezone)
	}
	if envelope.MissingFields == nil {
		envelope.MissingFields = []string{}
	}
	return json.Marshal(envelope)
}

func normalize(text string) string {
	return strings.ToLower(strings.TrimSpace(text))
}

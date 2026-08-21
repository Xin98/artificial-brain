package command

import (
	"context"
	"time"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

// maxDetailsPerOutcome caps how many decision lines per outcome a preview or
// report carries; the counts stay exact and Truncated reports the cap.
const maxDetailsPerOutcome = 100

// UploadImportHandler validates an uploaded export bundle, stores it as a
// pending import row, and returns the deterministic preview the user confirms
// against. The preview is stored on the row so GetImportQuery can serve it
// without re-parsing; confirm never reads it back.
type UploadImportHandler struct {
	Imports   ports.ImportStore
	Sources   ports.SourceRecordStore
	Parser    ports.BundleParser
	NewID     func() string
	Now       func() time.Time
	ImportTTL time.Duration // expiry is enforced by confirm and get; carried here for wiring symmetry
}

// Handle parses first — typed parser errors propagate and nothing is stored —
// then classifies every record against the source records already imported
// from the bundle's instance, stores the bundle bytes verbatim under a
// pending row, and returns the preview.
func (h *UploadImportHandler) Handle(ctx context.Context, principal ports.Principal, bundle []byte) (string, dto.Preview, error) {
	parsed, err := h.Parser.Parse(bundle)
	if err != nil {
		return "", dto.Preview{}, err
	}
	existing, err := h.Sources.Fingerprints(ctx, parsed.Manifest.SourceInstanceID, allRecordIDs(parsed))
	if err != nil {
		return "", dto.Preview{}, err
	}
	plan := classifyBundle(parsed, existing)
	preview := summarizeDecisions(plan.decisions)

	importID := h.NewID()
	row := dto.ImportRecordRow{
		ID:               importID,
		WorkspaceID:      principal.WorkspaceID,
		State:            dto.ImportStatePending,
		SourceInstanceID: parsed.Manifest.SourceInstanceID,
		Bundle:           bundle,
		Preview:          &preview,
		CreatedAt:        h.Now(),
	}
	if err := h.Imports.Save(ctx, row); err != nil {
		return "", dto.Preview{}, err
	}
	return importID, preview, nil
}

// bundlePlan is the classified view of a parsed bundle: the valid records in
// fixed kind order, every decision, and the fingerprints of the valid records
// keyed by source record id.
type bundlePlan struct {
	todos         []domain.TodoRecord
	channels      []domain.ChannelRecord
	deliveries    []domain.DeliveryRecord
	fingerprints  map[string]string // source record id -> content fingerprint (valid records)
	decisions     []dto.Decision
	decisionIndex map[string]int // kind + "\x00" + source record id -> decisions index
}

// classifyBundle validates every record, fingerprints the valid ones, and
// runs the decision engine against the existing fingerprints. Decision order
// is deterministic: Decide over the valid entries (todos, channels,
// deliveries), then the invalid decisions appended. Invalid records are the
// caller-side half of the domain contract — Decide never sees them.
func classifyBundle(parsed ports.ParsedBundle, existing map[string]string) *bundlePlan {
	plan := &bundlePlan{
		fingerprints:  map[string]string{},
		decisionIndex: map[string]int{},
	}
	var entries []domain.ImportEntry
	var invalid []dto.Decision

	for _, record := range parsed.Todos {
		if err := domain.ValidateTodoRecord(record); err != nil {
			invalid = append(invalid, invalidDecision(domain.KindTodo, record.ID, err))
			continue
		}
		plan.todos = append(plan.todos, record)
		fingerprint := domain.Fingerprint(record)
		plan.fingerprints[record.ID] = fingerprint
		entries = append(entries, domain.ImportEntry{Kind: domain.KindTodo, SourceRecordID: record.ID, Fingerprint: fingerprint})
	}
	for _, record := range parsed.Channels {
		if err := domain.ValidateChannelRecord(record); err != nil {
			invalid = append(invalid, invalidDecision(domain.KindChannel, record.ID, err))
			continue
		}
		plan.channels = append(plan.channels, record)
		fingerprint := domain.Fingerprint(record)
		plan.fingerprints[record.ID] = fingerprint
		entries = append(entries, domain.ImportEntry{Kind: domain.KindChannel, SourceRecordID: record.ID, Fingerprint: fingerprint})
	}
	for _, record := range parsed.Deliveries {
		if err := domain.ValidateDeliveryRecord(record); err != nil {
			invalid = append(invalid, invalidDecision(domain.KindDelivery, record.ID, err))
			continue
		}
		plan.deliveries = append(plan.deliveries, record)
		fingerprint := domain.Fingerprint(record)
		plan.fingerprints[record.ID] = fingerprint
		entries = append(entries, domain.ImportEntry{Kind: domain.KindDelivery, SourceRecordID: record.ID, Fingerprint: fingerprint})
	}

	for _, decision := range domain.Decide(entries, existing) {
		plan.appendDecision(dto.Decision{
			Kind:           decision.Kind,
			SourceRecordID: decision.SourceRecordID,
			Outcome:        string(decision.Outcome),
			Reason:         decision.Reason,
		})
	}
	for _, decision := range invalid {
		plan.appendDecision(decision)
	}
	return plan
}

func invalidDecision(kind, sourceRecordID string, err error) dto.Decision {
	return dto.Decision{Kind: kind, SourceRecordID: sourceRecordID, Outcome: string(domain.OutcomeInvalid), Reason: err.Error()}
}

func (p *bundlePlan) appendDecision(decision dto.Decision) {
	p.decisionIndex[decision.Kind+"\x00"+decision.SourceRecordID] = len(p.decisions)
	p.decisions = append(p.decisions, decision)
}

// outcomeOf reports a record's current decision outcome; execution may revise
// decisions (channel duplicates, orphan deliveries) before the report folds.
func (p *bundlePlan) outcomeOf(kind, sourceRecordID string) domain.Outcome {
	index, ok := p.decisionIndex[kind+"\x00"+sourceRecordID]
	if !ok {
		return ""
	}
	return domain.Outcome(p.decisions[index].Outcome)
}

// reviseDecision rewrites one record's outcome and reason in place.
func (p *bundlePlan) reviseDecision(kind, sourceRecordID string, outcome domain.Outcome, reason string) {
	index, ok := p.decisionIndex[kind+"\x00"+sourceRecordID]
	if !ok {
		return
	}
	p.decisions[index].Outcome = string(outcome)
	p.decisions[index].Reason = reason
}

// allRecordIDs lists every record id in the bundle — valid and invalid — in
// todos, channels, deliveries order; empty ids are dropped.
func allRecordIDs(parsed ports.ParsedBundle) []string {
	ids := make([]string, 0, len(parsed.Todos)+len(parsed.Channels)+len(parsed.Deliveries))
	appendID := func(id string) {
		if id != "" {
			ids = append(ids, id)
		}
	}
	for _, record := range parsed.Todos {
		appendID(record.ID)
	}
	for _, record := range parsed.Channels {
		appendID(record.ID)
	}
	for _, record := range parsed.Deliveries {
		appendID(record.ID)
	}
	return ids
}

// summarizeDecisions folds a decision list into preview counts plus the
// capped detail lines shared by previews and reports. Details keep at most
// maxDetailsPerOutcome lines per outcome, in decision order; Truncated
// reports that any outcome exceeded the cap.
func summarizeDecisions(decisions []dto.Decision) dto.Preview {
	preview := dto.Preview{Details: make([]dto.Decision, 0, len(decisions))}
	keptPerOutcome := map[string]int{}
	for _, decision := range decisions {
		switch decision.Outcome {
		case string(domain.OutcomeNew):
			preview.New++
		case string(domain.OutcomeSkipped):
			preview.Skipped++
		case string(domain.OutcomeConflict):
			preview.Conflicts++
		case string(domain.OutcomeInvalid):
			preview.Invalid++
		}
		if keptPerOutcome[decision.Outcome] < maxDetailsPerOutcome {
			preview.Details = append(preview.Details, decision)
			keptPerOutcome[decision.Outcome]++
			continue
		}
		preview.Truncated = true
	}
	return preview
}

// buildReport folds the final decision set into the committed import report.
func buildReport(decisions []dto.Decision, committedAt time.Time) dto.ImportReport {
	summary := summarizeDecisions(decisions)
	return dto.ImportReport{
		New:         summary.New,
		Skipped:     summary.Skipped,
		Conflicts:   summary.Conflicts,
		Invalid:     summary.Invalid,
		Details:     summary.Details,
		Truncated:   summary.Truncated,
		CommittedAt: committedAt,
	}
}

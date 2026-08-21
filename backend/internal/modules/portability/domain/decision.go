package domain

import "strings"

// Outcome classifies one bundle record during import.
type Outcome string

const (
	// OutcomeNew marks a record never imported from its source instance.
	OutcomeNew Outcome = "new"
	// OutcomeSkipped marks a record whose content fingerprint matches the
	// previously imported copy.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeConflict marks a record whose content fingerprint differs from
	// the previously imported copy.
	OutcomeConflict Outcome = "conflict"
	// OutcomeInvalid marks a record the caller rejected before or during
	// execution; the application layer constructs these decisions.
	OutcomeInvalid Outcome = "invalid"
)

// Import entry kinds carried by decisions.
const (
	KindTodo     = "todo"
	KindChannel  = "channel"
	KindDelivery = "delivery"
)

// ImportEntry is one validated bundle record awaiting a decision.
type ImportEntry struct {
	Kind           string // todo|channel|delivery
	SourceRecordID string
	Fingerprint    string
}

// Decision is the classification of one import entry.
type Decision struct {
	Kind           string
	SourceRecordID string
	Outcome        Outcome
	Reason         string
}

// Decide classifies validated entries against the source records already
// imported, keyed "sourceInstanceID:sourceRecordID" with the stored content
// fingerprint as value. An equal fingerprint ⇒ skipped, a different one ⇒
// conflict, an unseen record ⇒ new. Invalid entries are classified by the
// caller before Decide; output order is always the input order.
func Decide(entries []ImportEntry, existing map[string]string) []Decision {
	decisions := make([]Decision, 0, len(entries))
	for _, entry := range entries {
		decisions = append(decisions, decideEntry(entry, existing))
	}
	return decisions
}

func decideEntry(entry ImportEntry, existing map[string]string) Decision {
	if entry.SourceRecordID == "" {
		return Decision{Kind: entry.Kind, Outcome: OutcomeNew}
	}
	matched := false
	for key, fingerprint := range existing {
		if recordIDFromKey(key) != entry.SourceRecordID {
			continue
		}
		if fingerprint == entry.Fingerprint {
			return Decision{
				Kind:           entry.Kind,
				SourceRecordID: entry.SourceRecordID,
				Outcome:        OutcomeSkipped,
				Reason:         "fingerprint unchanged since last import",
			}
		}
		matched = true
	}
	if matched {
		return Decision{
			Kind:           entry.Kind,
			SourceRecordID: entry.SourceRecordID,
			Outcome:        OutcomeConflict,
			Reason:         "fingerprint changed since last import",
		}
	}
	return Decision{Kind: entry.Kind, SourceRecordID: entry.SourceRecordID, Outcome: OutcomeNew}
}

// recordIDFromKey extracts the source record id from a
// "sourceInstanceID:sourceRecordID" key; malformed keys yield "".
func recordIDFromKey(key string) string {
	_, recordID, found := strings.Cut(key, ":")
	if !found {
		return ""
	}
	return recordID
}

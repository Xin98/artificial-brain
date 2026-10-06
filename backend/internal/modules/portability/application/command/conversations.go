package command

import (
	"context"
	"errors"
	"sort"

	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/portability/domain"
)

func conversationRecordID(kind, id string) string { return kind + ":" + id }

func (p *bundlePlan) classifyConversations(parsed ports.ParsedBundle, existing map[string]string) {
	classify := func(kind, id string, record any, err error) {
		if err != nil {
			p.appendDecision(invalidDecision(kind, id, err))
			return
		}
		key := conversationRecordID(kind, id)
		fingerprint := domain.Fingerprint(record)
		p.fingerprints[key] = fingerprint
		d := domain.Decide([]domain.ImportEntry{{SourceInstanceID: parsed.Manifest.SourceInstanceID, Kind: kind, SourceRecordID: key, Fingerprint: fingerprint}}, existing)[0]
		p.appendDecision(dto.Decision{Kind: kind, SourceRecordID: id, Outcome: string(d.Outcome), Reason: d.Reason})
	}
	sessionCounts := map[string]int{}
	for _, r := range parsed.Sessions {
		sessionCounts[r.ID]++
	}
	seenSessions := map[string]bool{}
	for _, r := range parsed.Sessions {
		err := domain.ValidateSessionRecord(r)
		if sessionCounts[r.ID] > 1 {
			err = errors.New("duplicate session id")
		}
		if err == nil {
			seenSessions[r.ID] = true
		}
		classify(domain.KindSession, r.ID, r, err)
		if err == nil {
			p.sessions = append(p.sessions, r)
		}
	}
	messageCounts := map[string]int{}
	orderCounts := map[int64]int{}
	for _, r := range parsed.Messages {
		messageCounts[r.ID]++
		orderCounts[r.Order]++
	}
	for _, r := range parsed.Messages {
		err := domain.ValidateMessageRecord(r)
		if messageCounts[r.ID] > 1 || orderCounts[r.Order] > 1 {
			err = errors.New("duplicate message id or order")
		}
		if r.SessionID != nil && !seenSessions[*r.SessionID] {
			err = errors.New("session_not_found")
		}
		classify(domain.KindMessage, r.ID, r, err)
		if err == nil {
			p.messages = append(p.messages, r)
		}
	}
	sort.SliceStable(p.messages, func(i, j int) bool { return p.messages[i].Order < p.messages[j].Order })
}

func (h *ConfirmImportHandler) executeConversations(ctx context.Context, principal ports.Principal, source string, p *bundlePlan) error {
	if len(p.sessions) == 0 && len(p.messages) == 0 {
		return nil
	}
	if h.Conversations == nil {
		return errors.New("conversation importer is unavailable")
	}
	targets := map[string]string{}
	ids := make([]string, 0, len(p.sessions))
	for _, r := range p.sessions {
		ids = append(ids, conversationRecordID(domain.KindSession, r.ID))
	}
	old, err := h.Sources.ForOwner(principal).Targets(ctx, source, ids)
	if err != nil {
		return err
	}
	for _, r := range p.sessions {
		key := conversationRecordID(domain.KindSession, r.ID)
		if p.outcomeOf(domain.KindSession, r.ID) != domain.OutcomeNew {
			// A conflicting parent must never accept newly imported child rows.
			if p.outcomeOf(domain.KindSession, r.ID) == domain.OutcomeSkipped {
				targets[r.ID] = old[source+":"+key]
			}
			continue
		}
		target, err := h.Conversations.ImportSession(ctx, principal, r)
		if err != nil {
			return err
		}
		if err = h.Sources.ForOwner(principal).Register(ctx, newSourceRecord(principal.WorkspaceID, source, key, domain.KindSession, target, p.fingerprints[key])); err != nil {
			return err
		}
		targets[r.ID] = target
	}
	for _, r := range p.messages {
		if p.outcomeOf(domain.KindMessage, r.ID) != domain.OutcomeNew {
			continue
		}
		key := conversationRecordID(domain.KindMessage, r.ID)
		imported := r
		if r.SessionID != nil {
			target := targets[*r.SessionID]
			if target == "" {
				p.reviseDecision(domain.KindMessage, r.ID, domain.OutcomeInvalid, "session_not_found_or_conflict")
				continue
			}
			imported.SessionID = &target
		}
		target, err := h.Conversations.ImportMessage(ctx, principal, imported)
		if err != nil {
			if errors.Is(err, ports.ErrConversationSessionNotFound) {
				p.reviseDecision(domain.KindMessage, r.ID, domain.OutcomeInvalid, "session_not_found_or_conflict")
				continue
			}
			return err
		}
		if err = h.Sources.ForOwner(principal).Register(ctx, newSourceRecord(principal.WorkspaceID, source, key, domain.KindMessage, target, p.fingerprints[key])); err != nil {
			return err
		}
	}
	return nil
}

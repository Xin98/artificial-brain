package command

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"sort"
	"time"
)

type ReconcileAccountHandler struct {
	UOW      ports.UnitOfWork
	Accounts ports.AccountStore
	Orders   ports.OrderStore
	Ledger   ports.ReconciliationStore
	Catalog  ports.CatalogStore
	Data     ports.ResearchData
	Now      func() time.Time
	NewID    func() string
}

func (h ReconcileAccountHandler) Handle(ctx context.Context, r dto.ReconcileRequest) (dto.ReconcileResult, error) {
	var result dto.ReconcileResult
	e := h.UOW.Run(ctx, func(ctx context.Context) error { var e error; result, e = h.Apply(ctx, r); return e })
	return result, e
}
func (h ReconcileAccountHandler) Apply(ctx context.Context, r dto.ReconcileRequest) (dto.ReconcileResult, error) {
	out := dto.ReconcileResult{AccountID: r.AccountID, State: "completed", Blocked: []domain.Exclusion{}}
	now := h.Now()
	through := r.Through
	if through.IsZero() {
		through = now
	}
	if through.After(now) {
		return out, domain.ErrInvalidInput
	}
	account, e := h.Accounts.Lock(ctx, r.Scope, r.AccountID)
	if e != nil {
		return out, e
	}
	snapshot, e := h.Data.Read(ctx, account.Mode, through)
	if e != nil {
		return out, e
	}
	if e = domain.ValidateAccountDataset(account, snapshot); e != nil {
		return out, e
	}
	positions, e := h.Orders.LoadPositions(ctx, r.Scope, account.ID)
	if e != nil {
		return out, e
	}
	orders, e := h.Orders.ListPending(ctx, r.Scope, account.ID)
	if e != nil {
		return out, e
	}
	relevant := map[string]bool{}
	for _, p := range positions {
		if p.Quantity > 0 {
			relevant[p.InstrumentID] = true
		}
	}
	for _, o := range orders {
		relevant[o.InstrumentID] = true
	}
	if h.Catalog != nil {
		universe, e := h.Catalog.GetUniverse(ctx, r.Scope, account.UniverseVersionID)
		if e != nil {
			return out, e
		}
		for _, id := range universe.InstrumentIDs {
			relevant[id] = true
		}
	}
	changed := false
	appendEntries := func(entries []domain.LedgerEntry) error {
		for n := range entries {
			entries[n].ID = h.NewID()
			entries[n].RecordedAt = now
		}
		if e := h.Ledger.InsertLedger(ctx, r.Scope, account.ID, entries); e != nil {
			return e
		}
		out.AppliedEvents += len(entries)
		if len(entries) > 0 {
			changed = true
		}
		return nil
	}
	unsettled, e := h.Ledger.Unsettled(ctx, r.Scope, account.ID)
	if e != nil {
		return out, e
	}
	settled, e := domain.SettleReceivables(account, unsettled, snapshot.Calendar, through)
	if e != nil {
		return out, e
	}
	for _, entry := range settled.Entries {
		claimed, e := h.Ledger.ClaimEvent(ctx, r.Scope, account.ID, entry.EventKey)
		if e != nil {
			return out, e
		}
		if !claimed {
			return out, domain.ErrVersionConflict
		}
	}
	if e = appendEntries(settled.Entries); e != nil {
		return out, e
	}
	account = settled.Account
	actions := append([]domain.CorporateAction(nil), snapshot.Actions...)
	sort.Slice(actions, func(i, j int) bool {
		a, b := actions[i], actions[j]
		if !a.EffectiveAt.Equal(b.EffectiveAt) {
			return a.EffectiveAt.Before(b.EffectiveAt)
		}
		if a.Kind != b.Kind {
			if a.Kind == "split" {
				return true
			}
			if b.Kind == "split" {
				return false
			}
		}
		return a.ID < b.ID
	})
	block := func(a domain.CorporateAction, reason string) {
		out.State = "blocked"
		out.Blocked = append(out.Blocked, domain.Exclusion{InstrumentID: a.InstrumentID, Reason: reason})
	}
	for _, action := range actions {
		if !relevant[action.InstrumentID] || (!action.EffectiveAt.IsZero() && (action.EffectiveAt.Before(account.CreatedAt) || action.EffectiveAt.After(through))) {
			continue
		}
		recorded, recordErr := h.Ledger.RecordedAction(ctx, r.Scope, account.ID, action.ID)
		if recordErr != nil && !errors.Is(recordErr, domain.ErrNotFound) {
			if errors.Is(recordErr, domain.ErrCorporateActionIncomplete) {
				block(action, recordErr.Error())
				continue
			}
			return out, recordErr
		}
		if recordErr == nil && !domain.SameActionEconomics(recorded, action) {
			block(action, "corporate_action_revision_requires_review")
			continue
		}
		if errors.Is(recordErr, domain.ErrNotFound) {
			// A known pre-open split invalidates all old quantities, including an awaiting order when publication was delayed.
			if action.Kind == "split" {
				for n, o := range orders {
					if o.InstrumentID != action.InstrumentID || o.Terminal() || o.TargetOpenAt.Before(action.EffectiveAt) {
						continue
					}
					release, e := domain.ReleaseOrder(account, positions, o, "corporate_action_pre_open", now)
					if e != nil {
						return out, e
					}
					account = release.Account
					positions = release.Positions
					for n := range release.Entries {
						release.Entries[n].EffectiveAt = action.EffectiveAt
					}
					if e = appendEntries(release.Entries); e != nil {
						return out, e
					}
					o.State = domain.OrderCancelled
					o.Reason = "corporate_action_pre_open"
					o.ReservedCash = 0
					o.ReservedQuantity = 0
					if e = h.Orders.SaveOrder(ctx, r.Scope, account.ID, o, o.Version); e != nil {
						return out, e
					}
					o.Version++
					orders[n] = o
				}
				historyChanged, e := h.Ledger.InventoryChangedSince(ctx, r.Scope, account.ID, action.InstrumentID, action.EffectiveAt)
				if e != nil {
					return out, e
				}
				if historyChanged {
					block(action, "late_split_inventory_replay_requires_review")
					continue
				}
			}
			unresolved := false
			for _, o := range orders {
				if o.InstrumentID == action.InstrumentID && !o.Terminal() && o.TargetOpenAt.Before(action.EffectiveAt) {
					unresolved = true
				}
			}
			if unresolved {
				block(action, "prior_opening_fill_unresolved")
				continue
			}
			if _, e = domain.ApplyCorporateAction(account, positions, action, nil); e != nil {
				block(action, e.Error())
				continue
			}
			claimed, e := h.Ledger.ClaimEvent(ctx, r.Scope, account.ID, "action/"+action.ID)
			if e != nil {
				return out, e
			}
			if !claimed {
				return out, domain.ErrVersionConflict
			}
			if e = h.Ledger.SaveActionEligibility(ctx, r.Scope, account.ID, action, nil); e != nil {
				return out, e
			}
			eligibility, e := h.Ledger.ActionEligibility(ctx, r.Scope, account.ID, action.ID)
			if e != nil {
				return out, e
			}
			if e = h.Ledger.SaveActionEligibility(ctx, r.Scope, account.ID, action, eligibility); e != nil {
				return out, e
			}
			mutation, e := domain.ApplyCorporateAction(account, positions, action, eligibility)
			if e != nil {
				return out, e
			}
			account = mutation.Account
			positions = mutation.Positions
			if e = appendEntries(mutation.Entries); e != nil {
				return out, e
			}
		}
		if action.Kind == "dividend" && !through.Before(action.PayAt) {
			_, e := h.Ledger.EventEntry(ctx, r.Scope, account.ID, "payment/"+action.ID)
			if e == nil {
				continue
			}
			if !errors.Is(e, domain.ErrNotFound) {
				return out, e
			}
			accrual, e := h.Ledger.EventEntry(ctx, r.Scope, account.ID, "action/"+action.ID)
			if e != nil {
				return out, e
			}
			paid, e := domain.PayDividend(account, action, accrual, through)
			if e != nil {
				return out, e
			}
			claimed, e := h.Ledger.ClaimEvent(ctx, r.Scope, account.ID, "payment/"+action.ID)
			if e != nil {
				return out, e
			}
			if !claimed {
				return out, domain.ErrVersionConflict
			}
			account = paid.Account
			if e = appendEntries(paid.Entries); e != nil {
				return out, e
			}
		}
	}
	if changed {
		if e = h.Orders.SavePositions(ctx, r.Scope, account.ID, positions); e != nil {
			return out, e
		}
		if e = h.Accounts.Save(ctx, account, account.Version); e != nil {
			return out, e
		}
	}
	return out, nil
}

package query

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"time"
)

type AnalysisQuery struct {
	Data        ports.ResearchData
	Accounts    ports.AccountStore
	Catalog     ports.CatalogStore
	RiskBook    ports.RiskBookReader
	Mode        string
	NewsEnabled bool
	Now         func() time.Time
}

func (h AnalysisQuery) Handle(ctx context.Context, r dto.AnalysisRequest) (dto.AnalysisView, error) {
	out := dto.AnalysisView{Topics: []domain.Topic{}, QualityFlags: []string{}}
	var account domain.Account
	var e error
	if r.AccountID != "" {
		account, e = h.Accounts.Get(ctx, r.Scope, r.AccountID)
		if e != nil {
			return out, e
		}
	}
	if r.AsOf.IsZero() {
		r.AsOf = h.Now()
	}
	if r.AsOf.After(h.Now()) {
		return out, domain.ErrInvalidInput
	}
	snapshot, e := h.Data.Read(ctx, h.Mode, r.AsOf)
	if e != nil {
		return out, e
	}
	if r.AccountID != "" {
		if e = domain.ValidateAccountDataset(account, snapshot); e != nil {
			return out, e
		}
	}
	found := false
	for _, i := range snapshot.Instruments {
		if i.ID == r.InstrumentID {
			out.Instrument = i
			found = true
		}
	}
	if !found {
		return out, domain.ErrNotFound
	}
	out.Mode = snapshot.Mode
	out.Feed = snapshot.Feed
	out.DatasetVersion = snapshot.DatasetVersion
	out.AsOf = r.AsOf
	out.QualityFlags = append(out.QualityFlags, snapshot.QualityFlags...)
	var bars []domain.Bar
	var facts []domain.FinancialFact
	var actions []domain.CorporateAction
	var news []domain.NewsItem
	for _, b := range snapshot.Bars {
		if b.InstrumentID == r.InstrumentID {
			bars = append(bars, b)
		}
	}
	for _, f := range snapshot.Facts {
		if f.InstrumentID == r.InstrumentID {
			facts = append(facts, f)
		}
	}
	for _, a := range snapshot.Actions {
		if a.InstrumentID == r.InstrumentID {
			actions = append(actions, a)
		}
	}
	for _, n := range snapshot.News {
		for _, id := range n.InstrumentIDs {
			if id == r.InstrumentID {
				news = append(news, n)
				break
			}
		}
	}
	bars, e = domain.AdjustedBars(bars, actions, r.AsOf)
	if e != nil {
		out.QualityFlags = append(out.QualityFlags, e.Error())
		bars = nil
	}
	out.Metrics, e = domain.ComputeFinancialMetrics(facts, bars, r.AsOf)
	if e != nil {
		out.QualityFlags = append(out.QualityFlags, e.Error())
	}
	out.Risk = domain.ClassifyRisk(out.Metrics.Indicators)
	out.Risk.AsOf = r.AsOf
	strategy := domain.StrategyVersion{ID: "research/multifactor-v1", Parameters: domain.DefaultStrategyParameters()}
	universe := domain.UniverseVersion{ID: "research/current-pool"}
	for _, i := range snapshot.Instruments {
		if i.Kind == "stock" {
			universe.InstrumentIDs = append(universe.InstrumentIDs, i.ID)
		}
	}
	if r.AccountID != "" {
		strategy, e = h.Catalog.GetStrategy(ctx, r.Scope, account.StrategyVersionID)
		if e != nil {
			return out, e
		}
		universe, e = h.Catalog.GetUniverse(ctx, r.Scope, account.UniverseVersionID)
		if e != nil {
			return out, e
		}
	}
	evaluated, evalErr := domain.Evaluate(snapshot, universe, strategy)
	out.Recommendation = domain.Recommendation{Action: "insufficient_evidence", Potential: "unknown", AsOf: r.AsOf, StrategyVersionID: strategy.ID, UniverseVersionID: universe.ID, Evidence: []string{}, Unknowns: []string{}}
	if evalErr != nil {
		out.Recommendation.Unknowns = append(out.Recommendation.Unknowns, evalErr.Error())
	}
	for _, excluded := range evaluated.Excluded {
		if excluded.InstrumentID == r.InstrumentID {
			out.Recommendation.Unknowns = append(out.Recommendation.Unknowns, excluded.Reason)
		}
	}
	for n, s := range evaluated.Signals {
		if s.InstrumentID == r.InstrumentID {
			out.Signal = &evaluated.Signals[n]
			out.Recommendation = s.Recommendation
		}
	}
	if r.AccountID != "" {
		decision := domain.RiskDecision{ReasonCode: "account_risk_not_evaluated"}
		if h.RiskBook != nil {
			book, err := h.RiskBook.LoadRiskBook(ctx, r.Scope, r.AccountID, r.AsOf)
			if err != nil {
				return out, err
			}
			nav, err := domain.ComputeNAV(account, book.Positions, snapshot, nil)
			price := domain.Price(0)
			for _, bar := range bars {
				price = bar.Close
			}
			if err != nil {
				decision.ReasonCode = err.Error()
			} else if price <= 0 {
				decision.ReasonCode = "data_stale"
			} else {
				decision, err = domain.ValidateOrder(domain.OrderRiskInput{Account: account, Positions: book.Positions, Orders: book.Orders, Snapshot: snapshot, Evaluation: evaluated, InstrumentID: r.InstrumentID, Side: "buy", Quantity: 1, Price: price, NAV: nav, Policy: account.Policy, SessionTurnover: book.SessionTurnover, Automatic: true, Parameters: &strategy.Parameters})
				if err != nil {
					decision = domain.RiskDecision{ReasonCode: err.Error()}
				}
			}
		}
		out.AccountRisk = &decision
		if !decision.Allowed {
			out.Recommendation.Action = "account_buy_blocked"
			out.Recommendation.Unknowns = append(out.Recommendation.Unknowns, decision.ReasonCode)
		}
	}
	out.NewsStatus = "available"
	if snapshot.Mode != "fixture" && !h.NewsEnabled {
		out.NewsStatus = "not_configured"
		out.NewsReason = "news_permission_not_verified"
	}
	for _, flag := range snapshot.QualityFlags {
		if flag == "news_unavailable" {
			out.NewsStatus = "unavailable"
			out.NewsReason = flag
		}
		if flag == "news_not_configured" {
			out.NewsStatus = "not_configured"
			out.NewsReason = flag
		}
	}
	if out.NewsStatus == "available" {
		out.Topics, e = domain.AggregateTopics(news, r.AsOf)
		if e != nil {
			return out, e
		}
	}
	return out, nil
}

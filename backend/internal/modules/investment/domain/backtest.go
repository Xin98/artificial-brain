package domain

import (
	"context"
	"fmt"
	"sort"
	"time"
)

type BacktestInput struct {
	Snapshots   []Snapshot
	Calendar    Calendar
	Universe    UniverseVersion
	Strategy    StrategyVersion
	InitialCash Money
	From, To    time.Time
	BenchmarkID string
}
type ReplayEvent struct {
	Kind                    string
	EffectiveAt, RecordedAt time.Time
	Order                   *Order
	Fill                    *Fill
	Entries                 []LedgerEntry
}
type BacktestResult struct {
	Trace, BenchmarkTrace []ReplayEvent
	Curve, BenchmarkCurve []NAVPoint
	Fills, BenchmarkFills []Fill
	Metrics               Performance
	BenchmarkMetrics      *Performance
	QualityFlags          []string
	ExecutionModel        string
	BenchmarkReason       string
}
type replayBook struct {
	account       Account
	positions     []Position
	orders        []Order
	ledger        []LedgerEntry
	fills         []Fill
	trace         []ReplayEvent
	applied       map[string]CorporateAction
	paid, settled map[string]bool
	peak          Money
}

func newReplayBook(id string, in BacktestInput) *replayBook {
	return &replayBook{account: Account{ID: id, Mode: in.Snapshots[0].Mode, DatasetVersion: in.Snapshots[0].DatasetVersion, InitialCash: in.InitialCash, Balances: Balances{Available: in.InitialCash}, Policy: DefaultRiskPolicy(), AutomationEnabled: true}, positions: []Position{}, orders: []Order{}, ledger: []LedgerEntry{}, fills: []Fill{}, trace: []ReplayEvent{}, applied: map[string]CorporateAction{}, paid: map[string]bool{}, settled: map[string]bool{}, peak: in.InitialCash}
}
func (b *replayBook) apply(m LedgerMutation, kind string, at time.Time) {
	b.account = m.Account
	if m.Positions != nil {
		b.positions = m.Positions
	}
	b.ledger = append(b.ledger, m.Entries...)
	for _, l := range m.Entries {
		b.trace = append(b.trace, ReplayEvent{Kind: kind, EffectiveAt: l.EffectiveAt, RecordedAt: at, Entries: []LedgerEntry{l}})
	}
}
func (b *replayBook) reconcile(snapshot Snapshot, now time.Time, relevant map[string]bool) error {
	pending := []LedgerEntry{}
	for _, l := range b.ledger {
		if l.Kind == "sell_fill" && !b.settled[l.EventKey] {
			pending = append(pending, l)
		}
	}
	m, e := SettleReceivables(b.account, pending, snapshot.Calendar, now)
	if e != nil {
		return e
	}
	b.apply(m, "settlement", now)
	for _, l := range m.Entries {
		for _, p := range pending {
			if l.EventKey == "settlement/"+p.EventKey {
				b.settled[p.EventKey] = true
			}
		}
	}
	actions := append([]CorporateAction(nil), snapshot.Actions...)
	sort.Slice(actions, func(i, j int) bool {
		if !actions[i].EffectiveAt.Equal(actions[j].EffectiveAt) {
			return actions[i].EffectiveAt.Before(actions[j].EffectiveAt)
		}
		if actions[i].Kind != actions[j].Kind {
			return actions[i].Kind == "split"
		}
		return actions[i].ID < actions[j].ID
	})
	for _, a := range actions {
		if !relevant[a.InstrumentID] || a.EffectiveAt.After(now) {
			continue
		}
		prior, done := b.applied[a.ID]
		if done && !SameActionEconomics(prior, a) {
			return ErrCorporateActionIncomplete
		}
		if !done {
			if a.AvailableAt.After(a.EffectiveAt) {
				return ErrCorporateActionIncomplete
			}
			if a.Kind == "split" {
				for n, o := range b.orders {
					if !o.Terminal() && o.InstrumentID == a.InstrumentID && !o.TargetOpenAt.Before(a.EffectiveAt) {
						m, e = ReleaseOrder(b.account, b.positions, o, "split_cancel", now)
						if e != nil {
							return e
						}
						b.apply(m, "split_cancel", now)
						b.orders[n].State = OrderCancelled
					}
				}
			}
			eligible := []Position{}
			quantities := map[string]Quantity{}
			for _, l := range b.ledger {
				if l.EffectiveAt.Before(a.EffectiveAt) || (l.Kind == "split" && l.EffectiveAt.Equal(a.EffectiveAt)) {
					quantities[l.InstrumentID] += l.QuantityDelta
				}
			}
			for id, q := range quantities {
				eligible = append(eligible, Position{InstrumentID: id, Quantity: q})
			}
			m, e = ApplyCorporateAction(b.account, b.positions, a, eligible)
			if e != nil {
				return e
			}
			b.apply(m, "action", now)
			b.applied[a.ID] = a
		}
		if a.Kind == "dividend" && !b.paid[a.ID] && !now.Before(a.PayAt) {
			var accrual LedgerEntry
			for _, l := range b.ledger {
				if l.EventKey == "action/"+a.ID {
					accrual = l
					break
				}
			}
			m, e = PayDividend(b.account, a, accrual, now)
			if e != nil {
				return e
			}
			b.apply(m, "dividend_payment", now)
			b.paid[a.ID] = true
		}
	}
	return nil
}
func mergedReplaySource(in BacktestInput) (Snapshot, error) {
	if len(in.Snapshots) == 0 {
		return Snapshot{}, ErrDataStale
	}
	out := in.Snapshots[0]
	out.Bars = nil
	out.Facts = nil
	out.Actions = nil
	out.News = nil
	for _, s := range in.Snapshots {
		if s.DatasetVersion != out.DatasetVersion || s.Mode != out.Mode || s.Feed != out.Feed {
			return out, ErrVersionConflict
		}
		for _, f := range s.QualityFlags {
			if f == "corporate_action_publication_time_unknown" || f == "corporate_action_history_incomplete" || f == "delisting_terminal_value_missing" {
				return out, ErrCorporateActionIncomplete
			}
		}
		out.Bars = append(out.Bars, s.Bars...)
		out.Facts = append(out.Facts, s.Facts...)
		out.Actions = append(out.Actions, s.Actions...)
	}
	out.Calendar = in.Calendar
	return out, nil
}
func Replay(in BacktestInput) (BacktestResult, error) {
	return ReplayWithContext(context.Background(), in)
}
func ReplayWithContext(ctx context.Context, in BacktestInput) (BacktestResult, error) {
	out := BacktestResult{Trace: []ReplayEvent{}, BenchmarkTrace: []ReplayEvent{}, Curve: []NAVPoint{}, BenchmarkCurve: []NAVPoint{}, Fills: []Fill{}, BenchmarkFills: []Fill{}, QualityFlags: []string{"fixed_universe_survivorship_bias", "cash_yield_zero", "research_interval_not_out_of_sample"}, ExecutionModel: ExecutionModel}
	if in.InitialCash <= 0 || in.InitialCash > 100000000000 || in.To.Before(in.From) || len(in.Universe.InstrumentIDs) == 0 || len(in.Universe.InstrumentIDs) > 100 {
		return out, ErrInvalidInput
	}
	if e := in.Strategy.Parameters.Validate(); e != nil {
		return out, e
	}
	source, e := mergedReplaySource(in)
	if e != nil {
		return out, e
	}
	out.QualityFlags = append(out.QualityFlags, source.QualityFlags...)
	sessions := []Session{}
	all := append([]Session(nil), in.Calendar.Sessions...)
	sort.Slice(all, func(i, j int) bool { return all[i].OpenAt.Before(all[j].OpenAt) })
	var previous Session
	for _, s := range all {
		if s.Date.Before(in.From) {
			previous = s
		}
		if !s.Date.Before(in.From) && !s.Date.After(in.To) {
			sessions = append(sessions, s)
		}
	}
	if len(sessions) < 20 || previous.CloseAt.IsZero() {
		return out, ErrDataStale
	}
	warm, e := SelectSnapshot(source, previous.CloseAt.Add(30*time.Minute))
	if e != nil {
		return out, e
	}
	counts := map[string]int{}
	for _, b := range warm.Bars {
		if b.SessionDate.Before(in.From) {
			counts[b.InstrumentID]++
		}
	}
	relevant := map[string]bool{}
	for _, id := range in.Universe.InstrumentIDs {
		relevant[id] = true
		if counts[id] < 201 {
			return out, ErrDataStale
		}
	}
	book := newReplayBook("replay", in)
	benchmark := newReplayBook("benchmark", in)
	benchmarkOK := false
	for _, i := range source.Instruments {
		if i.ID == in.BenchmarkID && i.Ticker == "SPY" && counts[i.ID] >= 201 {
			benchmarkOK = true
		}
	}
	if !benchmarkOK {
		out.BenchmarkReason = "benchmark_history_incomplete"
	}
	for index, session := range sessions {
		if e = ctx.Err(); e != nil {
			return out, e
		}
		decisionAt := previous.CloseAt.Add(30 * time.Minute)
		decision, e := SelectSnapshot(source, decisionAt)
		if e != nil {
			return out, e
		}
		if e = book.reconcile(decision, decisionAt, relevant); e != nil {
			return out, e
		}
		if e = book.plan(decision, in, session); e != nil {
			return out, e
		}
		now := session.CloseAt.Add(30 * time.Minute)
		current, e := SelectSnapshot(source, now)
		if e != nil {
			return out, e
		}
		if e = book.reconcile(current, session.OpenAt, relevant); e != nil {
			return out, e
		}
		if e = book.execute(source, current, now); e != nil {
			return out, e
		}
		if e = book.reconcile(current, now, relevant); e != nil {
			return out, e
		}
		nav, e := ComputeNAV(book.account, book.positions, current, nil)
		if e != nil {
			return out, e
		}
		if nav > book.peak {
			book.peak = nav
		}
		out.Curve = append(out.Curve, NAVPoint{SessionDate: session.Date, NAV: nav})
		if benchmarkOK {
			if e = benchmark.reconcile(current, session.OpenAt, map[string]bool{in.BenchmarkID: true}); e == nil && index == 0 {
				e = benchmark.buyBenchmark(decision, current, session, now, in.BenchmarkID)
			}
			if e == nil {
				e = benchmark.reconcile(current, now, map[string]bool{in.BenchmarkID: true})
			}
			var n Money
			if e == nil {
				n, e = ComputeNAV(benchmark.account, benchmark.positions, current, nil)
			}
			if e != nil {
				benchmarkOK = false
				out.BenchmarkReason = "benchmark_history_incomplete"
				out.BenchmarkCurve = []NAVPoint{}
			} else {
				out.BenchmarkCurve = append(out.BenchmarkCurve, NAVPoint{SessionDate: session.Date, NAV: n})
			}
		}
		previous = session
	}
	out.Trace = book.trace
	out.Fills = book.fills
	// Final performance uses economic dates. A late confirmation cannot leave an earlier cash-only NAV behind.
	out.Curve, e = book.economicCurve(source, sessions)
	if e != nil {
		return out, e
	}
	for _, f := range book.fills {
		if dateUTC(f.RecordedAt).After(dateUTC(f.EffectiveAt)) {
			out.QualityFlags = append(out.QualityFlags, "late_confirmation_nav_restated")
			break
		}
	}
	out.Metrics, e = ComputePerformance(out.Curve, out.Fills, in.InitialCash)
	if e != nil {
		return out, e
	}
	if benchmarkOK {
		out.BenchmarkCurve, e = benchmark.economicCurve(source, sessions)
		if e != nil {
			return out, e
		}
		out.BenchmarkTrace = benchmark.trace
		out.BenchmarkFills = benchmark.fills
		p, err := ComputePerformance(out.BenchmarkCurve, benchmark.fills, in.InitialCash)
		if err != nil {
			return out, err
		}
		out.BenchmarkMetrics = &p
	}
	return out, nil
}
func (b *replayBook) economicCurve(source Snapshot, sessions []Session) ([]NAVPoint, error) {
	out := []NAVPoint{}
	finalAt := sessions[len(sessions)-1].CloseAt.Add(30 * time.Minute)
	for _, session := range sessions {
		cutoff := session.CloseAt.Add(30 * time.Minute)
		a := b.account
		a.Balances = Balances{Available: a.InitialCash}
		quantities := map[string]Quantity{}
		for _, l := range b.ledger {
			if l.EffectiveAt.After(cutoff) {
				continue
			}
			var e error
			a.Balances, e = a.Balances.Apply(l.Delta)
			if e != nil {
				return nil, e
			}
			quantities[l.InstrumentID] += l.QuantityDelta
		}
		positions := []Position{}
		for id, q := range quantities {
			if q < 0 {
				return nil, ErrInvalidInput
			}
			if q > 0 {
				positions = append(positions, Position{InstrumentID: id, Quantity: q})
			}
		}
		next, e := source.Calendar.NextSession(session.CloseAt)
		if e != nil {
			return nil, e
		}
		availableThrough := next.CloseAt
		if availableThrough.After(finalAt) {
			availableThrough = finalAt
		}
		known, e := SelectSnapshot(source, availableThrough)
		if e != nil {
			return nil, e
		}
		prices := map[string]Price{}
		for _, v := range known.Bars {
			if v.SessionDate.Equal(session.Date) && v.Close > 0 {
				prices[v.InstrumentID] = v.Close
			}
		}
		for _, p := range positions {
			if prices[p.InstrumentID] <= 0 {
				return nil, ErrDataStale
			}
		}
		known.AsOf = cutoff
		nav, e := ComputeNAV(a, positions, known, prices)
		if e != nil {
			return nil, e
		}
		out = append(out, NAVPoint{SessionDate: session.Date, NAV: nav})
	}
	return out, nil
}
func (b *replayBook) plan(s Snapshot, in BacktestInput, target Session) error {
	if !b.account.AutomationEnabled {
		return nil
	}
	for _, o := range b.orders {
		if !o.Terminal() {
			return nil
		}
	}
	evaluation, e := Evaluate(s, in.Universe, in.Strategy)
	if e != nil {
		return e
	}
	nav, e := ComputeNAV(b.account, b.positions, s, nil)
	if e != nil {
		return e
	}
	plan, e := BuildRebalance(PortfolioInput{Account: b.account, Positions: b.positions, Orders: b.orders, Snapshot: s, Evaluation: evaluation, Policy: b.account.Policy, NAV: nav, PeakNAV: b.peak, Parameters: &in.Strategy.Parameters})
	if e != nil {
		return e
	}
	if plan.Pause {
		b.account.AutomationEnabled = false
		return nil
	}
	following, e := s.Calendar.NextSession(target.CloseAt)
	if e != nil {
		return e
	}
	cash := b.account.Balances.Available
	for _, p := range plan.Orders {
		bars := []Bar{}
		close := Price(0)
		for _, v := range s.Bars {
			if v.InstrumentID == p.InstrumentID {
				bars = append(bars, v)
				close = v.Close
			}
		}
		cap, e := PreviousVolumeCapacity(bars, target.OpenAt)
		if e != nil {
			return e
		}
		o := Order{ID: fmt.Sprintf("replay/%s/%s/%s", target.Date.Format("2006-01-02"), p.InstrumentID, p.Side), AccountID: b.account.ID, InstrumentID: p.InstrumentID, Side: p.Side, Quantity: p.Quantity, State: OrderPending, Reason: p.Reason, Origin: "automatic", Capacity: cap, TargetOpenAt: target.OpenAt, ExpiresAt: following.CloseAt, CreatedAt: s.AsOf, Version: 1}
		if o.Side == "buy" {
			lo, hi := Quantity(0), o.Quantity
			for lo < hi {
				mid := lo + (hi-lo+1)/2
				cost, err := ReservationCost(close, mid)
				if err != nil {
					return err
				}
				if cost <= cash {
					lo = mid
				} else {
					hi = mid - 1
				}
			}
			if lo == 0 {
				continue
			}
			o.Quantity = lo
			o.ReservedCash, e = ReservationCost(close, o.Quantity)
			if e != nil {
				return e
			}
			cash -= o.ReservedCash
		} else {
			o.ReservedQuantity = o.Quantity
		}
		m, e := ReserveOrders(b.account, b.positions, []Order{o})
		if e != nil {
			return e
		}
		b.apply(m, "reservation", s.AsOf)
		b.orders = append(b.orders, o)
		copy := o
		b.trace = append(b.trace, ReplayEvent{Kind: "order", EffectiveAt: o.TargetOpenAt, RecordedAt: s.AsOf, Order: &copy})
	}
	return nil
}
func (b *replayBook) execute(source, current Snapshot, now time.Time) error {
	type executionDay struct {
		prior    Snapshot
		prices   map[string]Price
		bars     map[string]Bar
		used     map[string]Quantity
		turnover Money
	}
	days := map[string]*executionDay{}
	sort.SliceStable(b.orders, func(i, j int) bool {
		if !b.orders[i].TargetOpenAt.Equal(b.orders[j].TargetOpenAt) {
			return b.orders[i].TargetOpenAt.Before(b.orders[j].TargetOpenAt)
		}
		if b.orders[i].Side != b.orders[j].Side {
			return b.orders[i].Side == "sell"
		}
		return b.orders[i].ID < b.orders[j].ID
	})
	for n, o := range b.orders {
		if o.Terminal() || now.Before(o.TargetOpenAt) {
			continue
		}
		b.orders[n] = AdvanceOrder(o, now)
		o = b.orders[n]
		key := o.TargetOpenAt.UTC().Format(time.RFC3339)
		day := days[key]
		if day == nil {
			prior, e := SelectSnapshot(source, o.TargetOpenAt.Add(-time.Nanosecond))
			if e != nil {
				return e
			}
			day = &executionDay{prior: prior, prices: map[string]Price{}, bars: map[string]Bar{}, used: map[string]Quantity{}}
			for _, v := range current.Bars {
				if v.SessionDate.Equal(dateUTC(o.TargetOpenAt)) {
					day.bars[v.InstrumentID] = v
					if v.Open > 0 && !v.NoOpeningTrade {
						day.prices[v.InstrumentID] = v.Open
					}
				}
			}
			for _, f := range b.fills {
				if f.EffectiveAt.Equal(o.TargetOpenAt) {
					day.used[f.InstrumentID] += f.Quantity
					day.turnover += f.Gross
				}
			}
			days[key] = day
		}
		v, exists := day.bars[o.InstrumentID]
		if !exists || v.NoOpeningTrade || v.Open <= 0 || v.AvailableAt.After(o.ExpiresAt) {
			if !now.Before(o.ExpiresAt) {
				m, e := ReleaseOrder(b.account, b.positions, o, "expired", now)
				if e != nil {
					return e
				}
				b.apply(m, "expiry", now)
				b.orders[n].State = OrderExpired
			}
			continue
		}
		nav, e := ComputeNAV(b.account, b.positions, day.prior, day.prices)
		if e != nil {
			return e
		}
		limit := o.Capacity
		for _, other := range b.orders {
			if other.TargetOpenAt.Equal(o.TargetOpenAt) && other.InstrumentID == o.InstrumentID && other.Capacity < limit {
				limit = other.Capacity
			}
		}
		capacity := limit - day.used[o.InstrumentID]
		if capacity < 0 {
			capacity = 0
		}
		matched, e := MatchAtOpen(MatchInput{Order: o, OpenPrice: v.Open, AvailableCapacity: capacity, RecordedAt: now, Portfolio: PortfolioInput{Account: b.account, Positions: b.positions, Orders: b.orders, Snapshot: day.prior, Prices: day.prices, NAV: nav, Policy: b.account.Policy, SessionTurnover: day.turnover}})
		if e != nil {
			return e
		}
		if matched.Fill == nil {
			m, e := ReleaseOrder(b.account, b.positions, o, "rejected", now)
			if e != nil {
				return e
			}
			b.apply(m, "rejected", now)
			b.orders[n].State = OrderRejected
			continue
		}
		f := *matched.Fill
		f.ID = "fill/" + o.ID
		m, e := ApplyFill(b.account, b.positions, f)
		if e != nil {
			return e
		}
		b.apply(m, "fill", now)
		for n := range b.positions {
			for _, i := range current.Instruments {
				if i.ID == b.positions[n].InstrumentID {
					b.positions[n].Industry = Industry(i.SIC)
				}
			}
		}
		b.orders[n].State = OrderFilled
		if matched.CancelledQuantity > 0 {
			b.orders[n].State = OrderPartial
		}
		day.used[f.InstrumentID] += f.Quantity
		day.turnover += f.Gross
		b.fills = append(b.fills, f)
		copy := f
		b.trace = append(b.trace, ReplayEvent{Kind: "execution", EffectiveAt: f.EffectiveAt, RecordedAt: f.RecordedAt, Fill: &copy})
	}
	return nil
}
func (b *replayBook) buyBenchmark(prior, current Snapshot, session Session, now time.Time, id string) error {
	var bar Bar
	bars := []Bar{}
	for _, v := range prior.Bars {
		if v.InstrumentID == id {
			bars = append(bars, v)
		}
	}
	for _, v := range current.Bars {
		if v.InstrumentID == id && v.SessionDate.Equal(session.Date) {
			bar = v
			break
		}
	}
	if bar.Open <= 0 || bar.NoOpeningTrade {
		return ErrDataStale
	}
	price, e := SlippedPrice(bar.Open, "buy")
	if e != nil {
		return e
	}
	cap, e := PreviousVolumeCapacity(bars, session.OpenAt)
	if e != nil {
		return e
	}
	low, high := Quantity(0), cap
	for low < high {
		mid := low + (high-low+1)/2
		cost, err := purchaseCost(price, mid)
		if err != nil {
			return err
		}
		if cost <= b.account.Balances.Available {
			low = mid
		} else {
			high = mid - 1
		}
	}
	if low == 0 {
		return ErrInsufficientCash
	}
	gross, e := GrossValue(price, low)
	if e != nil {
		return e
	}
	fee, e := TradeFee(gross)
	if e != nil {
		return e
	}
	o := Order{ID: "benchmark/buy", AccountID: b.account.ID, InstrumentID: id, Side: "buy", Quantity: low, ReservedCash: gross + fee, State: OrderPending, Capacity: cap, CreatedAt: prior.AsOf, TargetOpenAt: session.OpenAt, ExpiresAt: session.CloseAt.Add(24 * time.Hour), Version: 1}
	m, e := ReserveOrders(b.account, b.positions, []Order{o})
	if e != nil {
		return e
	}
	b.apply(m, "reservation", prior.AsOf)
	f := Fill{ID: "benchmark/fill", AccountID: b.account.ID, OrderID: o.ID, InstrumentID: id, Side: "buy", Quantity: low, Price: price, Gross: gross, Fee: fee, ReservedCash: o.ReservedCash, EffectiveAt: session.OpenAt, RecordedAt: now}
	m, e = ApplyFill(b.account, b.positions, f)
	if e != nil {
		return e
	}
	b.apply(m, "fill", now)
	b.fills = append(b.fills, f)
	return nil
}

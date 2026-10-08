// Package investmentcompose contains concrete wiring shared by API and worker executables.
package investmentcompose

import (
	"context"
	"crypto/rand"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/alpaca"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	store "github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/postgres"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/sec"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"sync"
	"time"
)

type Runtime struct {
	Mode, DatasetVersion string
	NewsEnabled          bool
	UOW                  ports.UnitOfWork
	Mutations            application.MutationExecutor
	Accounts             *store.AccountStore
	Catalog              *store.CatalogStore
	Orders               *store.OrderStore
	Ledger               *store.LedgerStore
	Runs                 *store.RunStore
	Snapshots            *store.SnapshotStore
	Data                 ports.ResearchData
	History              ports.HistoryData
	Reservations         command.ReservationWriter
	Evaluate             *command.EvaluateAccountHandler
	Jobs                 *command.InvestmentJobHandler
	Now                  func() time.Time
	NewID                func() string
}

func New(cfg config.InvestmentConfig, pool *pgxpool.Pool, now func() time.Time) (*Runtime, error) {
	if cfg.Mode == "" {
		cfg.Mode = "fixture"
	}
	if cfg.Feed == "" {
		cfg.Feed = "iex"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Second
	}
	if now == nil {
		now = time.Now
	}
	r := &Runtime{Mode: cfg.Mode, NewsEnabled: cfg.NewsEnabled, Now: now, NewID: newID, UOW: database.NewTxRunner(pool), Accounts: store.NewAccountStore(pool), Catalog: store.NewCatalogStore(pool), Orders: store.NewOrderStore(pool), Ledger: store.NewLedgerStore(pool), Runs: store.NewRunStore(pool), Snapshots: store.NewSnapshotStore(pool)}
	r.Mutations = application.MutationExecutor{UOW: r.UOW, Requests: store.NewRequestStore(pool)}
	source := &sourceService{cfg: cfg, r: r, financialAt: map[string]time.Time{}}
	if cfg.Mode == "fixture" {
		data, e := fixture.New()
		if e != nil {
			return nil, e
		}
		r.Data = data
		r.History = data
		r.DatasetVersion = data.Snapshot.DatasetVersion
		source.fixture = data
	} else {
		r.DatasetVersion = command.DatasetVersion(cfg.Mode, cfg.Feed, "v1")
		r.Data = store.ResearchReader{Store: r.Snapshots, Mode: r.Mode, DatasetVersion: r.DatasetVersion}
		r.History = store.ResearchReader{Store: r.Snapshots, Mode: r.Mode, DatasetVersion: r.DatasetVersion}
	}
	r.Reservations = command.ReservationWriter{Accounts: r.Accounts, Orders: r.Orders, Ledger: r.Ledger, Now: now, NewID: r.NewID}
	r.Evaluate = &command.EvaluateAccountHandler{Mutations: r.Mutations, UOW: r.UOW, Accounts: r.Accounts, Catalog: r.Catalog, Orders: r.Orders, Ledger: r.Ledger, Runs: r.Runs, Snapshots: r.Snapshots, Data: r.Data, Reservations: r.Reservations, Now: now, NewID: r.NewID}
	r.Jobs = &command.InvestmentJobHandler{UOW: r.UOW, Accounts: r.Accounts, Orders: r.Orders, Ledger: r.Ledger, Runs: r.Runs, SyncRuns: r.Runs, Source: source, Data: r.Data, Evaluate: r.Evaluate, Execute: command.ExecuteOrdersHandler{UOW: r.UOW, Accounts: r.Accounts, Orders: r.Orders, Ledger: r.Ledger, Actions: r.Ledger, Data: r.Data, Now: now, NewID: r.NewID}, Reconcile: command.ReconcileAccountHandler{UOW: r.UOW, Accounts: r.Accounts, Orders: r.Orders, Ledger: r.Ledger, Catalog: r.Catalog, Data: r.Data, Now: now, NewID: r.NewID}, Now: now, NewID: r.NewID}
	r.Jobs.Backtest = &command.RunBacktestHandler{UOW: r.UOW, Runs: r.Runs, Catalog: r.Catalog, Snapshots: r.Snapshots, Now: now}
	return r, nil
}
func (r *Runtime) SetScheduler(s ports.JobScheduler) { r.Evaluate.Scheduler = s; r.Jobs.Scheduler = s }
func newID() string {
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	b[6] = (b[6] & 15) | 64
	b[8] = (b[8] & 63) | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}

type sourceService struct {
	cfg         config.InvestmentConfig
	r           *Runtime
	fixture     *fixture.Adapter
	mu          sync.Mutex
	financialAt map[string]time.Time
}

type currentMarket struct {
	ports.MarketDataPort
	instruments []domain.Instrument
}

func (m *currentMarket) Instruments(ctx context.Context, ids []string) ([]domain.Instrument, error) {
	items, e := m.MarketDataPort.Instruments(ctx, ids)
	if e == nil {
		m.instruments = append([]domain.Instrument(nil), items...)
	}
	return items, e
}

func (s *sourceService) Sync(ctx context.Context, request dto.SyncRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	r := s.r
	if request.Mode != r.Mode || request.DatasetVersion != r.DatasetVersion {
		return domain.ErrVersionConflict
	}
	var market ports.MarketDataPort
	if s.fixture != nil {
		market = s.fixture
	} else {
		var calendar *domain.Calendar
		if s.cfg.SettlementCalendarFile != "" {
			v, e := alpaca.LoadSettlementCalendar(s.cfg.SettlementCalendarFile)
			if e != nil {
				return e
			}
			calendar = &v
		}
		v, e := alpaca.NewMarket(&http.Client{}, alpaca.MarketConfig{DataURL: "https://data.alpaca.markets", ReadOnlyTradingURL: "https://paper-api.alpaca.markets", Feed: s.cfg.Feed, Key: s.cfg.AlpacaKey, Secret: s.cfg.AlpacaSecret, Timeout: s.cfg.Timeout, SettlementCalendar: calendar})
		if e != nil {
			return e
		}
		market = v
	}
	feed := s.cfg.Feed
	if s.fixture != nil {
		feed = "synthetic"
	}
	current := &currentMarket{MarketDataPort: market}
	marketHandler := command.SyncMarketHandler{Market: current, Store: r.Snapshots, UOW: r.UOW, Mode: r.Mode, Feed: feed, DatasetVersion: r.DatasetVersion, Now: r.Now}
	if _, e := marketHandler.Handle(ctx, request); e != nil {
		return e
	}
	data, e := r.Data.Read(ctx, r.Mode, r.Now())
	if e != nil {
		return e
	}
	// Symbols bind to this provider response, never an older security that
	// reused the same ticker in the persisted research history.
	resolved := []string{}
	for _, wanted := range request.InstrumentIDs {
		for _, i := range current.instruments {
			if i.ID == wanted || i.Ticker == wanted {
				resolved = append(resolved, i.ID)
				break
			}
		}
	}
	request.InstrumentIDs = resolved
	financialIDs := []string{}
	for _, id := range request.InstrumentIDs {
		if r.Now().Sub(s.financialAt[id]) >= 24*time.Hour {
			financialIDs = append(financialIDs, id)
		}
	}
	if s.fixture != nil || len(financialIDs) > 0 {
		var facts ports.FinancialDataPort
		var identity ports.FinancialIdentityPort
		if s.fixture != nil {
			facts = s.fixture
		} else {
			v, e := sec.NewFinancials(&http.Client{}, sec.SECConfig{BaseURL: "https://data.sec.gov", UserAgent: s.cfg.SECUserAgent, Timeout: s.cfg.Timeout, Calendar: data.Calendar})
			if e != nil {
				return e
			}
			facts = v
			identity = v
		}
		req := request
		req.InstrumentIDs = financialIDs
		h := command.SyncFinancialsHandler{Financials: facts, Identity: identity, Data: r.Data, Store: r.Snapshots, Metadata: r.Snapshots, UOW: r.UOW, Mode: r.Mode, DatasetVersion: r.DatasetVersion, Now: r.Now}
		if _, e = h.Handle(ctx, req); e != nil {
			return e
		}
		for _, id := range financialIDs {
			s.financialAt[id] = r.Now()
		}
	}
	var news ports.NewsPort
	if s.fixture != nil {
		news = s.fixture
	} else {
		data, e = r.Data.Read(ctx, r.Mode, r.Now())
		if e != nil {
			return e
		}
		v, err := alpaca.NewNews(&http.Client{}, alpaca.NewsConfig{Enabled: s.cfg.NewsEnabled, BaseURL: "https://data.alpaca.markets", Key: s.cfg.AlpacaKey, Secret: s.cfg.AlpacaSecret, Timeout: s.cfg.Timeout, Instruments: data.Instruments})
		if err != nil {
			return err
		}
		news = v
	}
	h := command.SyncNewsHandler{News: news, Store: r.Snapshots, UOW: r.UOW, Mode: r.Mode, DatasetVersion: r.DatasetVersion, Now: r.Now}
	status, e := h.Handle(ctx, request)
	// News is informational and never changes the trading score. Its failure is persisted as a quality flag.
	if status.ErrorCode == "news_unavailable" || status.ErrorCode == "news_not_configured" {
		return nil
	}
	return e
}

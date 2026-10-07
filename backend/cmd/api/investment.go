package main

import (
	"github.com/Xin98/artificial-brain/backend/cmd/internal/investmentcompose"
	investmenthttp "github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/inbound/http"
	store "github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/postgres"
	investmentriver "github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/river"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/query"
	"github.com/Xin98/artificial-brain/backend/internal/platform/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	riverqueue "github.com/riverqueue/river"
	"net/http"
	"time"
)

func buildInvestmentHandler(cfg config.InvestmentConfig, pool *pgxpool.Pool, client *riverqueue.Client[pgx.Tx], now func() time.Time) *investmenthttp.Handler {
	runtime, e := investmentcompose.New(cfg, pool, now)
	if e != nil {
		panic("investment: invalid fixture composition")
	}
	scheduler := &investmentriver.Scheduler{Client: client}
	runtime.SetScheduler(scheduler)
	reads := store.NewReadStore(pool)
	return &investmenthttp.Handler{
		Mode:           runtime.Mode,
		CreateAccount:  command.CreateAccountHandler{Mutations: runtime.Mutations, Accounts: runtime.Accounts, Catalog: runtime.Catalog, Ledger: runtime.Ledger, Data: runtime.Data, Now: runtime.Now, NewID: runtime.NewID},
		Account:        query.AccountsQuery{Accounts: runtime.Accounts, Orders: runtime.Orders, Store: reads, Now: runtime.Now},
		List:           query.ListQuery{Accounts: runtime.Accounts, Store: reads},
		Instruments:    query.InstrumentsQuery{Data: runtime.Data, Accounts: runtime.Accounts, Catalog: runtime.Catalog, Mode: runtime.Mode, Now: runtime.Now},
		DataStatus:     query.DataStatusQuery{Data: runtime.Data, Store: reads, Mode: runtime.Mode, NewsEnabled: runtime.NewsEnabled, Now: runtime.Now},
		Analysis:       query.AnalysisQuery{Data: runtime.Data, Accounts: runtime.Accounts, Catalog: runtime.Catalog, RiskBook: runtime.Orders, Mode: runtime.Mode, NewsEnabled: runtime.NewsEnabled, Now: runtime.Now},
		CreateUniverse: command.CreateUniverseVersionHandler{Mutations: runtime.Mutations, Catalog: runtime.Catalog, Data: runtime.Data, Now: runtime.Now, NewID: runtime.NewID},
		CreateStrategy: command.CreateStrategyVersionHandler{Mutations: runtime.Mutations, Catalog: runtime.Catalog, Now: runtime.Now, NewID: runtime.NewID},
		PlaceOrder:     command.PlaceOrderHandler{Mutations: runtime.Mutations, Accounts: runtime.Accounts, Orders: runtime.Orders, Data: runtime.Data, Reservations: runtime.Reservations, Now: runtime.Now, NewID: runtime.NewID},
		CancelOrder:    command.CancelOrderHandler{Mutations: runtime.Mutations, Accounts: runtime.Accounts, Orders: runtime.Orders, Ledger: runtime.Ledger, Now: runtime.Now, NewID: runtime.NewID},
		Automation:     command.ConfigureAutomationHandler{Mutations: runtime.Mutations, Accounts: runtime.Accounts, Catalog: runtime.Catalog, Events: runtime.Ledger, Data: runtime.Data, Reservations: &runtime.Reservations, Now: runtime.Now, NewID: runtime.NewID},
		StartSync:      command.StartSyncHandler{Mutations: runtime.Mutations, Runs: runtime.Runs, Scheduler: scheduler, Mode: runtime.Mode, DatasetVersion: runtime.DatasetVersion, Now: runtime.Now, NewID: runtime.NewID},
		Sync:           query.SyncQuery{Runs: runtime.Runs}, Evaluate: runtime.Evaluate,
		Evaluation:     query.EvaluationQuery{Accounts: runtime.Accounts, Runs: runtime.Runs},
		CreateBacktest: command.CreateBacktestHandler{Mutations: runtime.Mutations, Runs: runtime.Runs, Catalog: runtime.Catalog, Snapshots: runtime.Snapshots, History: runtime.History, Scheduler: scheduler, Mode: runtime.Mode, Now: runtime.Now, NewID: runtime.NewID},
		Backtest:       query.BacktestQuery{Runs: runtime.Runs},
		Performance:    query.PerformanceQuery{Accounts: runtime.Accounts, Store: reads, History: runtime.History, Now: runtime.Now},
	}
}
func registerInvestmentRoutes(cfg config.Config, pool *pgxpool.Pool, mux *http.ServeMux, auth func(http.Handler) http.Handler, client *riverqueue.Client[pgx.Tx]) {
	investmenthttp.RegisterRoutes(mux, auth, buildInvestmentHandler(cfg.Investment, pool, client, time.Now))
}

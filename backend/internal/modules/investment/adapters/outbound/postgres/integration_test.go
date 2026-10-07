package postgres

import (
	"context"
	"errors"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInvestmentMigrationTenToEleven(t *testing.T) {
	raw := os.Getenv("TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	conn, e := pgx.Connect(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer conn.Close(ctx)
	name := fmt.Sprintf("investment_upgrade_%d", time.Now().UnixNano())
	if _, e = conn.Exec(ctx, `create database `+name); e != nil {
		t.Fatal(e)
	}
	defer conn.Exec(ctx, `drop database `+name+` with (force)`)
	u, e := url.Parse(raw)
	if e != nil {
		t.Fatal(e)
	}
	u.Path = "/" + name
	dir := filepath.Join("..", "..", "..", "..", "..", "..", "..", "deploy", "migrations")
	old := t.TempDir()
	files, e := os.ReadDir(dir)
	if e != nil {
		t.Fatal(e)
	}
	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".sql") || f.Name() > "010_zzzz.sql" {
			continue
		}
		b, e := os.ReadFile(filepath.Join(dir, f.Name()))
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(filepath.Join(old, f.Name()), b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e = database.RunMigrations(ctx, u.String(), old); e != nil {
		t.Fatal(e)
	}
	upgrade, e := pgx.Connect(ctx, u.String())
	if e != nil {
		t.Fatal(e)
	}
	defer upgrade.Close(ctx)
	var version int
	if e = upgrade.QueryRow(ctx, "select version from public.schema_version").Scan(&version); e != nil || version != 10 {
		t.Fatal(version, e)
	}
	_, e = upgrade.Exec(ctx, `insert into todo.todos(id,workspace_id,owner_user_id,title) values('aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa',$1,$2,'upgrade sentinel')`, owner.WorkspaceID, owner.OwnerUserID)
	if e != nil {
		t.Fatal(e)
	}
	for range 2 {
		if e = database.RunMigrations(ctx, u.String(), dir); e != nil {
			t.Fatal(e)
		}
	}
	if e = upgrade.QueryRow(ctx, "select version from public.schema_version").Scan(&version); e != nil || version != 11 {
		t.Fatal(version, e)
	}
	var title string
	if e = upgrade.QueryRow(ctx, `select title from todo.todos where id='aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa'`).Scan(&title); e != nil || title != "upgrade sentinel" {
		t.Fatal(title, e)
	}
}

var owner = domain.Scope{WorkspaceID: "11111111-1111-4111-8111-111111111111", OwnerUserID: "22222222-2222-4222-8222-222222222222"}

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	if e := database.RunMigrations(ctx, url, filepath.Join("..", "..", "..", "..", "..", "..", "..", "deploy", "migrations")); e != nil {
		t.Fatal(e)
	}
	p, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	return p
}
func TestInvestmentSchemaAndOwnerIsolation(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	for _, name := range []string{"accounts", "universe_versions", "universe_members", "strategy_versions", "datasets", "data_snapshots", "data_sync_runs", "instruments", "instrument_facts", "price_bars", "financial_facts", "news_items", "corporate_actions", "evaluation_runs", "signals", "recommendations", "backtest_runs", "orders", "fills", "ledger_entries", "positions", "nav_snapshots", "automation_events", "mutation_requests"} {
		var exists bool
		if e := p.QueryRow(ctx, "select exists(select 1 from information_schema.tables where table_schema='investment' and table_name=$1)", name).Scan(&exists); e != nil || !exists {
			t.Fatal(name, e)
		}
	}
	s := NewAccountStore(p)
	id := "33333333-3333-4333-8333-333333333333"
	_, _ = p.Exec(ctx, "delete from investment.accounts where id=$1", id)
	a := domain.Account{ID: id, Scope: owner, Mode: "fixture", Name: "test", Version: 1, InitialCash: 10000, Balances: domain.Balances{Available: 10000}, Policy: domain.DefaultRiskPolicy(), CreatedAt: time.Now().UTC()}
	if e := s.Insert(ctx, a); e != nil {
		t.Fatal(e)
	}
	defer p.Exec(ctx, "delete from investment.accounts where id=$1", id)
	wrong := owner
	wrong.OwnerUserID = "44444444-4444-4444-8444-444444444444"
	if _, e := s.Get(ctx, wrong, id); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
	wrong = owner
	wrong.WorkspaceID = "44444444-4444-4444-8444-444444444444"
	if _, e := s.Get(ctx, wrong, id); !errors.Is(e, domain.ErrNotFound) {
		t.Fatal(e)
	}
	if e := s.Save(ctx, a, 0); !errors.Is(e, domain.ErrVersionConflict) {
		t.Fatal(e)
	}
	catalog := NewCatalogStore(p)
	v := domain.StrategyVersion{ID: "55555555-5555-4555-8555-555555555555", StrategyID: "multifactor-v1", Scope: owner, Parameters: domain.DefaultStrategyParameters(), CreatedAt: time.Now().UTC()}
	_, _ = p.Exec(ctx, "delete from investment.strategy_versions where id=$1", v.ID)
	if e := catalog.InsertStrategy(ctx, owner, v); e != nil {
		t.Fatal(e)
	}
	defer p.Exec(ctx, "delete from investment.strategy_versions where id=$1", v.ID)
	if _, e := p.Exec(ctx, "update investment.strategy_versions set parameters=parameters where id=$1", v.ID); e == nil {
		t.Fatal("mutable version")
	}
}
func TestInvestmentTransactionRollback(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	s := NewAccountStore(p)
	l := NewLedgerStore(p)
	id := "66666666-6666-4666-8666-666666666666"
	_, _ = p.Exec(ctx, "delete from investment.accounts where id=$1", id)
	tx := database.NewTxRunner(p)
	sentinel := errors.New("intentional abort")
	e := tx.Run(ctx, func(ctx context.Context) error {
		a := domain.Account{ID: id, Scope: owner, Mode: "fixture", Name: "rollback", Version: 1, InitialCash: 100, Balances: domain.Balances{Available: 100}, Policy: domain.DefaultRiskPolicy(), CreatedAt: time.Now().UTC()}
		if e := s.Insert(ctx, a); e != nil {
			return e
		}
		if e := l.InsertLedger(ctx, owner, id, []domain.LedgerEntry{{ID: "77777777-7777-4777-8777-777777777777", AccountID: id, EventKey: "initial", Kind: "initial_credit", Delta: domain.Balances{Available: 100}, EffectiveAt: time.Now().UTC(), RecordedAt: time.Now().UTC()}}); e != nil {
			return e
		}
		return sentinel
	})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	for _, table := range []string{"accounts", "ledger_entries"} {
		var count int
		column := "account_id"
		if table == "accounts" {
			column = "id"
		}
		if e := p.QueryRow(ctx, "select count(*) from investment."+table+" where "+column+"=$1", id).Scan(&count); e != nil || count != 0 {
			t.Fatal(table, count, e)
		}
	}
}

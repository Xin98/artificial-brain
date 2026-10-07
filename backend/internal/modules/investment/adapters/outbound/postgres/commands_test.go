package postgres

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/adapters/outbound/fixture"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/command"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"github.com/Xin98/artificial-brain/backend/internal/platform/database"
	"sync"
	"testing"
	"time"
)

func uuid() string {
	var b [16]byte
	if _, e := rand.Read(b[:]); e != nil {
		panic(e)
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:])
}
func TestCreateAccountIdempotent(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	data, e := fixture.New()
	if e != nil {
		t.Fatal(e)
	}
	now := func() time.Time { return time.Date(2026, 10, 6, 21, 0, 0, 0, time.UTC) }
	exec := application.MutationExecutor{UOW: database.NewTxRunner(p), Requests: NewRequestStore(p)}
	h := command.CreateAccountHandler{Mutations: exec, Accounts: NewAccountStore(p), Catalog: NewCatalogStore(p), Ledger: NewLedgerStore(p), Data: data, Now: now, NewID: uuid}
	r := dto.CreateAccountRequest{Mutation: dto.Mutation{Scope: owner, Route: "accounts", Key: uuid()}, InitialCash: "100000.00", Mode: "fixture"}
	first, e := h.Handle(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	defer p.Exec(ctx, "delete from investment.accounts where id=$1", first.ID)
	defer p.Exec(ctx, "delete from investment.mutation_requests where workspace_id=$1 and owner_user_id=$2 and key=$3", owner.WorkspaceID, owner.OwnerUserID, r.Key)
	retry, e := h.Handle(ctx, r)
	if e != nil || first.ID != retry.ID || retry.AutomationEnabled {
		t.Fatal(retry, e)
	}
	var count int
	var cash int64
	e = p.QueryRow(ctx, "select count(*),sum(available_delta) from investment.ledger_entries where account_id=$1", first.ID).Scan(&count, &cash)
	if e != nil || count != 1 || cash != 10000000 {
		t.Fatal(count, cash, e)
	}
	r.InitialCash = "200000.00"
	if _, e = h.Handle(ctx, r); !errors.Is(e, domain.ErrIdempotencyConflict) {
		t.Fatal(e)
	}
	automation := command.ConfigureAutomationHandler{Mutations: exec, Accounts: NewAccountStore(p), Catalog: NewCatalogStore(p), Events: NewLedgerStore(p), Data: data, Now: now, NewID: uuid}
	config := dto.ConfigureAutomationRequest{Mutation: dto.Mutation{Scope: owner, Route: "automation/" + first.ID, Key: uuid(), ExpectedVersion: 1}, AccountID: first.ID, Enabled: true, StrategyVersionID: first.StrategyVersionID, UniverseVersionID: first.UniverseVersionID, Policy: domain.DefaultRiskPolicy(), Mode: "alpaca_sec"}
	if _, e = automation.Handle(ctx, config); !errors.Is(e, domain.ErrInvalidInput) {
		t.Fatal("mode changed", e)
	}
	config.Key = uuid()
	config.Mode = "fixture"
	copyData := *data
	copyData.Snapshot.Calendar.SettlementDays = nil
	automation.Data = &copyData
	if _, e = automation.Handle(ctx, config); !errors.Is(e, domain.ErrDataNotConfigured) {
		t.Fatal("missing settlement calendar", e)
	}
	config.Key = uuid()
	automation.Data = data
	view, e := automation.Handle(ctx, config)
	if e != nil || !view.AutomationEnabled || view.Version != 2 {
		t.Fatal(view, e)
	}
}
func TestConcurrentClaimAndRollbackRetry(t *testing.T) {
	p := testPool(t)
	ctx := context.Background()
	exec := application.MutationExecutor{UOW: database.NewTxRunner(p), Requests: NewRequestStore(p)}
	m := dto.Mutation{Scope: owner, Route: "concurrency-test", Key: uuid()}
	var mu sync.Mutex
	calls := 0
	work := func(context.Context) (dto.AccountView, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return dto.AccountView{ID: "original"}, nil
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, e := application.RunMutation(exec, ctx, m, "same", work)
			if e == nil && v.ID != "original" {
				e = fmt.Errorf("wrong response")
			}
			errs <- e
		}()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if calls != 1 {
		t.Fatal(calls)
	}
	m.Key = uuid()
	sentinel := errors.New("infra failure")
	_, e := application.RunMutation(exec, ctx, m, "same", func(context.Context) (dto.AccountView, error) { return dto.AccountView{}, sentinel })
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	if _, e = application.RunMutation(exec, ctx, m, "same", work); e != nil {
		t.Fatal(e)
	}
}

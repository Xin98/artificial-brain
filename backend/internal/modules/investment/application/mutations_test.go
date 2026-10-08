package application

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"testing"
)

type immediateTx struct{}

func (immediateTx) Run(ctx context.Context, f func(context.Context) error) error { return f(ctx) }

type requestMemory struct{ records map[string]dto.RequestRecord }

func (m *requestMemory) Claim(ctx context.Context, s domain.Scope, route, key, hash string) (dto.RequestRecord, error) {
	k := s.WorkspaceID + s.OwnerUserID + route + key
	if r, ok := m.records[k]; ok {
		return r, nil
	}
	r := dto.RequestRecord{Hash: hash, Claimed: true}
	m.records[k] = r
	return r, nil
}
func (m *requestMemory) Complete(ctx context.Context, s domain.Scope, route, key string, b json.RawMessage) error {
	k := s.WorkspaceID + s.OwnerUserID + route + key
	r := m.records[k]
	r.Response = b
	m.records[k] = r
	return nil
}
func TestCreateAccountIdempotentMutationBoundary(t *testing.T) {
	ctx := context.Background()
	store := &requestMemory{map[string]dto.RequestRecord{}}
	exec := MutationExecutor{UOW: immediateTx{}, Requests: store}
	m := dto.Mutation{Scope: domain.Scope{WorkspaceID: "w", OwnerUserID: "u"}, Route: "accounts", Key: "key"}
	calls := 0
	work := func(context.Context) (json.RawMessage, error) { calls++; return json.RawMessage(`{"id":"a"}`), nil }
	first, e := exec.Execute(ctx, m, map[string]string{"cash": "100000.00"}, work)
	if e != nil {
		t.Fatal(e)
	}
	retry, e := exec.Execute(ctx, m, map[string]string{"cash": "100000.00"}, work)
	if e != nil || string(first) != string(retry) || calls != 1 {
		t.Fatal(first, retry, e, calls)
	}
	_, e = exec.Execute(ctx, m, map[string]string{"cash": "200000.00"}, work)
	if !errors.Is(e, domain.ErrIdempotencyConflict) {
		t.Fatal(e)
	}
}
func TestBusinessRejectionPersistsBeforeReturningError(t *testing.T) {
	store := &requestMemory{map[string]dto.RequestRecord{}}
	exec := MutationExecutor{UOW: immediateTx{}, Requests: store}
	m := dto.Mutation{Scope: domain.Scope{WorkspaceID: "w", OwnerUserID: "u"}, Route: "orders", Key: "denied"}
	_, e := RunMutation(exec, context.Background(), m, "input", func(context.Context) (dto.AccountView, error) { return dto.AccountView{}, domain.ErrRiskLimit })
	if !errors.Is(e, domain.ErrRiskLimit) {
		t.Fatal(e)
	}
	if len(store.records) != 1 {
		t.Fatal(store.records)
	}
	for _, r := range store.records {
		if len(r.Response) == 0 {
			t.Fatal("rejection lost")
		}
	}
}

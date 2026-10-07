package postgres

import (
	"context"
	"errors"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/query"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"testing"
)

func TestInvestmentCursorDatabaseNoDuplicates(t *testing.T) {
	h := newOrderHarness(t)
	ctx := context.Background()
	original, err := h.accounts.Get(ctx, h.scope, h.account.ID)
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 29; n++ {
		a := original
		a.ID = uuid()
		a.Name = "分页账户"
		if e := h.accounts.Insert(ctx, a); e != nil {
			t.Fatal(e)
		}
	}
	q := query.ListQuery{Accounts: h.accounts, Store: NewReadStore(h.pool)}
	r := dto.ListRequest{Scope: h.scope, Resource: "accounts"}
	p, e := q.Handle(ctx, r)
	if e != nil || len(p.Items) != 25 || p.NextCursor == "" {
		t.Fatal(len(p.Items), e)
	}
	seen := map[string]bool{}
	for _, item := range p.Items {
		seen[item.(map[string]any)["id"].(string)] = true
	}
	r.Cursor = p.NextCursor
	p, e = q.Handle(ctx, r)
	if e != nil || len(p.Items) != 5 || p.NextCursor != "" {
		t.Fatal(len(p.Items), e)
	}
	for _, item := range p.Items {
		id := item.(map[string]any)["id"].(string)
		if seen[id] {
			t.Fatal("duplicate", id)
		}
		seen[id] = true
	}
	r.Scope.OwnerUserID = uuid()
	if _, e = q.Handle(ctx, r); !errors.Is(e, domain.ErrInvalidInput) {
		t.Fatal(e)
	}
	r.Scope = h.scope
	r.Resource = "orders"
	r.ParentID = h.account.ID
	if _, e = q.Handle(ctx, r); !errors.Is(e, domain.ErrInvalidInput) {
		t.Fatal(e)
	}
}

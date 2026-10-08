package query

import (
	"context"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"testing"
	"time"
)

type listStub struct {
	ports.ReadStore
	rows []dto.ListRow
}

func (s listStub) ListRows(_ context.Context, r dto.ListRequest, after dto.PageCursor) ([]dto.ListRow, error) {
	out := []dto.ListRow{}
	for _, v := range s.rows {
		if after.ID != "" && v.ID >= after.ID {
			continue
		}
		out = append(out, v)
		if len(out) >= r.Limit {
			break
		}
	}
	return out, nil
}
func TestInvestmentCursorNoDuplicates(t *testing.T) {
	rows := []dto.ListRow{}
	for n := 99; n >= 70; n-- {
		id := string(rune(n))
		rows = append(rows, dto.ListRow{ID: id, At: time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), Value: id})
	}
	q := ListQuery{Store: listStub{rows: rows}}
	r := dto.ListRequest{Scope: domain.Scope{WorkspaceID: "w", OwnerUserID: "u"}, Resource: "accounts"}
	first, e := q.Handle(context.Background(), r)
	if e != nil || len(first.Items) != 25 || first.NextCursor == "" {
		t.Fatal(first, e)
	}
	r.Cursor = first.NextCursor
	second, e := q.Handle(context.Background(), r)
	if e != nil || len(second.Items) != 5 || second.NextCursor != "" {
		t.Fatal(second, e)
	}
	r.Scope.OwnerUserID = "other"
	if _, e = q.Handle(context.Background(), r); e == nil {
		t.Fatal("cross owner cursor")
	}
	r.Scope.OwnerUserID = "u"
	r.ParentID = "other"
	if _, e = q.Handle(context.Background(), r); e == nil {
		t.Fatal("cross parent cursor")
	}
	r.Cursor = ""
	r.Limit = 101
	if _, e = q.Handle(context.Background(), r); e == nil {
		t.Fatal("unbounded page")
	}
}

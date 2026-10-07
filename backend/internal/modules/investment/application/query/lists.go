package query

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/dto"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/application/ports"
	"github.com/Xin98/artificial-brain/backend/internal/modules/investment/domain"
	"strings"
)

type ListQuery struct {
	Store    ports.ReadStore
	Accounts ports.AccountStore
}

func cursorQuery(r dto.ListRequest) string { return r.Search + "/" + r.Risk + "/" + r.Potential }
func decodeCursor(r dto.ListRequest) (dto.PageCursor, error) {
	c := dto.PageCursor{}
	if r.Cursor == "" {
		return c, nil
	}
	if len(r.Cursor) > 4096 {
		return c, domain.ErrInvalidInput
	}
	b, e := base64.RawURLEncoding.DecodeString(r.Cursor)
	if e != nil {
		return c, domain.ErrInvalidInput
	}
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&c); e != nil {
		return c, domain.ErrInvalidInput
	}
	if c.Scope != r.Scope || c.Resource != r.Resource || c.ParentID != r.ParentID || c.Query != cursorQuery(r) || c.ID == "" || len(c.ID) > 200 {
		return c, domain.ErrInvalidInput
	}
	return c, nil
}
func pageRows(r dto.ListRequest, rows []dto.ListRow) dto.Page {
	out := dto.Page{Items: []any{}}
	more := len(rows) > r.Limit
	if more {
		rows = rows[:r.Limit]
	}
	for _, v := range rows {
		out.Items = append(out.Items, dto.Public(v.Value))
	}
	if more && len(rows) > 0 {
		last := rows[len(rows)-1]
		b, _ := json.Marshal(dto.PageCursor{Scope: r.Scope, Resource: r.Resource, ParentID: r.ParentID, Query: cursorQuery(r), ID: last.ID, At: last.At})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(b)
	}
	return out
}
func (h ListQuery) Handle(ctx context.Context, r dto.ListRequest) (dto.Page, error) {
	if r.Limit == 0 {
		r.Limit = 25
	}
	if r.Limit < 1 || r.Limit > 100 {
		return dto.Page{}, domain.ErrInvalidInput
	}
	switch r.Resource {
	case "accounts", "universes", "strategies", "backtests", "data-syncs":
		if r.ParentID != "" {
			return dto.Page{}, domain.ErrInvalidInput
		}
	case "positions", "orders", "ledger", "evaluations", "automation-events":
		if r.ParentID == "" {
			return dto.Page{}, domain.ErrInvalidInput
		}
	default:
		return dto.Page{}, domain.ErrInvalidInput
	}
	after, e := decodeCursor(r)
	if e != nil {
		return dto.Page{}, e
	}
	if r.ParentID != "" {
		if _, e = h.Accounts.Get(ctx, r.Scope, r.ParentID); e != nil {
			return dto.Page{}, e
		}
	}
	fetch := r
	fetch.Limit++
	rows, e := h.Store.ListRows(ctx, fetch, after)
	if e != nil {
		return dto.Page{}, e
	}
	return pageRows(r, rows), nil
}
